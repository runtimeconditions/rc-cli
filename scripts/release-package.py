"""Build relocatable CLI archives and Linux packages from the locked components."""

import argparse
import base64
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request

import yaml


VERSION_RE = re.compile(r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$")
COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")


def run(args, *, cwd=None, env=None):
    subprocess.run(args, cwd=cwd, env=env, check=True)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def copy_file(source, destination, mode=None):
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, destination)
    os.chmod(destination, mode if mode is not None else source.stat().st_mode & 0o777)


def add_tree(archive, root):
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"staged package contains a symlink: {path}")
        if path.is_file():
            data = path.read_bytes()
            info = tarfile.TarInfo(path.relative_to(root).as_posix())
            info.size = len(data)
            info.mode = path.stat().st_mode & 0o777
            info.mtime = 0
            info.uid = info.gid = 0
            info.uname = info.gname = ""
            archive.addfile(info, io.BytesIO(data))


def make_archive(root, output):
    with output.open("wb") as raw:
        with gzip.GzipFile(fileobj=raw, mode="wb", mtime=0, filename="") as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as archive:
                add_tree(archive, root)


def make_completions(binary):
    paths = {
        "bash": "bash-completion/completions/rc",
        "zsh": "zsh/site-functions/_rc",
        "fish": "fish/vendor_completions.d/rc.fish",
    }
    return {
        relative: subprocess.check_output([str(binary), "completion", shell], text=True)
        for shell, relative in paths.items()
    }


def stage_completions(share, completions):
    for relative, output in completions.items():
        destination = share / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text(output)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    match = VERSION_RE.fullmatch(args.version)
    if not match or not COMMIT_RE.fullmatch(args.commit):
        raise ValueError("release requires a stable vMAJOR.MINOR.PATCH tag and full commit SHA")
    if subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip() != args.commit:
        raise ValueError("checked-out source commit differs from the release event")
    run(["git", "merge-base", "--is-ancestor", args.commit, "origin/main"])
    if args.output.exists() and any(args.output.iterdir()):
        raise ValueError("release output directory must be new or empty")
    args.output.mkdir(parents=True, exist_ok=True)

    lock = yaml.safe_load(Path("distribution/components.lock.yaml").read_text())
    extensions = lock["extensions"]
    core = lock["goProfiler"]["coreSchema"]
    with urllib.request.urlopen(core["url"], timeout=60) as response:
        core_schema = response.read(64 * 1024 * 1024 + 1)
    if len(core_schema) > 64 * 1024 * 1024 or sha256(core_schema) != core["sha256"]:
        raise ValueError("downloaded core profile schema does not match the component lock")

    with tempfile.TemporaryDirectory(prefix="rc-cli-release-") as temporary:
        work = Path(temporary)
        checkout = work / "extensions"
        checkout.mkdir()
        run(["git", "init", "--quiet", str(checkout)])
        run(["git", "-C", str(checkout), "fetch", "--depth=1", extensions["repository"], extensions["commit"]])
        run(["git", "-C", str(checkout), "checkout", "--quiet", "--detach", "FETCH_HEAD"])
        if subprocess.check_output(["git", "-C", str(checkout), "rev-parse", "HEAD"], text=True).strip() != extensions["commit"]:
            raise ValueError("extensions checkout differs from the component lock")
        schema_dir = checkout / "tooling/extension-bindings/model"
        schemas = sorted(schema_dir.glob("*.schema.yaml"))
        if not schemas or any(item.is_symlink() or not item.is_file() for item in schemas):
            raise ValueError("locked extensions revision has no valid binding schemas")
        schema_hashes = "".join(f"{sha256(item.read_bytes())}  model/{item.name}\n" for item in schemas)
        core_path = work / "runtimeconditions.profile.schema.yaml"
        core_path.write_bytes(core_schema)

        archive_hashes = {}
        completions = None
        for goos in ("linux", "darwin"):
            for goarch in ("amd64", "arm64"):
                target = f"{goos}_{goarch}"
                env = dict(os.environ, CGO_ENABLED="0", GOOS=goos, GOARCH=goarch)
                binary = work / target / "bin/rc"
                binary.parent.mkdir(parents=True)
                linker_flags = "-X main.version=" + args.version + " -X main.commit=" + args.commit + \
                    " -X github.com/runtimeconditions/go-rc-profiler/extensioncheck.installedCoreSchemaBase64=" + \
                    base64.b64encode(core_schema).decode("ascii")
                run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags", linker_flags,
                     "-o", str(binary), "./cmd/rc"], env=env)
                prefix = work / target / "prefix"
                copy_file(binary, prefix / "bin/rc", 0o755)
                for schema in schemas:
                    copy_file(schema, prefix / "share/rc/extension-bindings/model" / schema.name, 0o644)
                (prefix / "share/rc/extension-bindings/SHA256SUMS").write_text(schema_hashes)
                copy_file(Path("distribution/components.lock.yaml"), prefix / "share/rc-cli/components.lock.yaml")
                copy_file(Path("LICENSE"), prefix / "share/doc/rc-cli/copyright")
                if completions is None:
                    completions = make_completions(binary)
                stage_completions(prefix / "share", completions)
                archive_path = args.output / f"rc-cli_{args.version[1:]}_{goos}_{goarch}.tar.gz"
                make_archive(prefix, archive_path)
                archive_hashes[f"{goos.upper()}_{goarch.upper()}_SHA256"] = sha256(archive_path.read_bytes())

                if goos == "linux":
                    package_root = work / target / "package-root"
                    usr_root = package_root / "usr"
                    copy_file(binary, usr_root / "bin/rc", 0o755)
                    shutil.copytree(prefix / "share", usr_root / "share")
                    env.update(GOARCH=goarch, PACKAGE_VERSION=match.group(0).lstrip("v"))
                    nfpm_config = Path("distribution/nfpm.yaml").resolve()
                    for packager in ("deb", "rpm"):
                        run(["nfpm", "package", "--config", str(nfpm_config), "--packager", packager,
                             "--target", str(args.output.resolve())], cwd=package_root, env=env)

    template = Path("distribution/homebrew/rc-cli.rb").read_text()
    rendered = template.replace("{{VERSION}}", args.version[1:])
    for key, value in archive_hashes.items():
        rendered = rendered.replace("{{" + key + "}}", value)
    if "{{" in rendered:
        raise ValueError("Homebrew formula template has unresolved release fields")
    (args.output / "rc-cli.rb").write_text(rendered)
    manifest = sorted(path for path in args.output.iterdir() if path.name != "SHA256SUMS")
    (args.output / "SHA256SUMS").write_text("".join(f"{sha256(path.read_bytes())}  {path.name}\n" for path in manifest))


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, yaml.YAMLError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"release-package: {error}")
