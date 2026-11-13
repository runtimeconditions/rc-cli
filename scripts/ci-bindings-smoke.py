"""Exercise a relocated CLI package, generated bindings, and offline Go profiling."""

import argparse
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
from threading import Thread

import yaml


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def command(args, cwd, env, expected=0):
    result = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True, timeout=120)
    require(result.returncode == expected, f"{args}: exit {result.returncode}, expected {expected}\n{result.stdout}\n{result.stderr}")
    return result


def tree(directory):
    return {path.relative_to(directory).as_posix(): path.read_bytes() for path in directory.rglob("*") if path.is_file()}


class NoPublishedReleases(BaseHTTPRequestHandler):
    def do_GET(self):
        valid = self.path == "/repos/example/ci-bindings/releases?per_page=100&page=1"
        self.server.paths.append(self.path)
        self.send_response(200 if valid else 404)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b"[]" if valid else b"{}")

    def log_message(self, *_args):
        pass


def smoke(args, root):
    prefix = root / "install"
    shutil.copytree(args.prefix, prefix)
    binary = str(prefix / "bin/rc")
    lock = yaml.safe_load((prefix / "share/rc-cli/components.lock.yaml").read_text())
    core = args.core_schema.read_bytes()
    require(hashlib.sha256(core).hexdigest() == lock["goProfiler"]["coreSchema"]["sha256"], "core schema checksum differs from the component lock")
    project = root / "project"
    (project / "catalog").mkdir(parents=True)
    shutil.copyfile(args.fixture, project / "catalog/runtimeconditions.extension.yaml")
    (project / "core.yaml").write_bytes(core)
    fixture = yaml.safe_load(args.fixture.read_text())
    tools = root / "tools"
    tools.mkdir()
    go = shutil.which("go")
    require(go, "Go toolchain is unavailable")
    (tools / "go").symlink_to(Path(go).resolve())
    env = {key: value for key, value in os.environ.items() if key not in {"GH_TOKEN", "GITHUB_TOKEN", "RC_BINDINGS_GITHUB_API"}}
    env.update(PATH=str(tools), GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="-mod=readonly -buildvcs=false", CGO_ENABLED="0", GOPROXY="off", GOSUMDB="off", GOPRIVATE="", GONOPROXY="", GONOSUMDB="", GOMODCACHE=str(root / "modules"), GOPATH=str(root / "gopath"))
    env.update(RC_BINDINGS_GO="go", RC_BINDINGS_PYTHON="/must-not-run", RC_GO_PROFILER_BIN="/must-not-run", RC_BINDINGS_NORMALIZER="/must-not-run", RC_BINDINGS_GO_EMITTER="/must-not-run")
    native = command(["go", "env", "GOVERSION"], project, env).stdout.strip()
    require(native == args.go_version, f"native Go version {native}, expected {args.go_version}")
    empty_path = dict(env, PATH=str(root / "no-tools"))

    def rc(*values, expected=0, selected=env, cwd=project):
        return command([binary, *values], cwd, selected, expected)

    info = yaml.safe_load(rc("version", "--output", "yaml").stdout)
    profiler = next(item for item in info["components"] if item["module"] == "github.com/runtimeconditions/go-rc-profiler")
    require(profiler["origin"] == "linked" and profiler["version"] == lock["goProfiler"]["version"], "linked profiler differs from the component lock")
    for values in [("--help",), ("bindings", "--help"), ("profile", "go", "--help")]:
        require("Usage:" in rc(*values, selected=empty_path).stdout, "help requires unavailable tools")
    catalog = {
        "apiVersion": "runtimeconditions.io/package-catalog/v1alpha1", "kind": "RuntimeConditionsPackageCatalog",
        "repositoryUrl": "https://github.com/example/ci-bindings", "packages": {"fixture": {
            "rootExtension": fixture["metadata"]["id"], "coreProfileSchemaVersion": lock["goProfiler"]["coreSchema"]["version"],
            "languages": {"go": {"coordinate": "example.test/bindings/fixture", "name": "fixture", "version": fixture["metadata"]["version"],
                "sourceDirectory": "bindings/fixture/go", "languageVersion": native.removeprefix("go"), "publicationMode": "github-tag"}},
        }},
    }
    (project / "packages.yaml").write_text(yaml.safe_dump(catalog))
    (project / "toolchain.lock.yaml").write_text("apiVersion: runtimeconditions.io/toolchain-lock/v1alpha1\nkind: RuntimeConditionsToolchainLock\nstatus: development\n")
    binding_flags = ("--core-schema", str(project / "core.yaml"))
    for name in ("resolve", "normalize"):
        require("parity.service" in rc("bindings", name, *binding_flags, selected=empty_path).stdout, f"{name} omitted the fixture kind")
    require(not (project / "bindings").exists(), "read-only commands created committed bindings")
    generated = root / "generated"
    rc("bindings", "generate", "--output", str(generated), *binding_flags, selected=empty_path)
    summary = yaml.safe_load(rc("bindings", "verify", "--input", str(generated), *binding_flags).stdout)
    require(summary["status"] == "passed", "generated tree verification failed")
    rc("bindings", "update", *binding_flags)
    package = project / "bindings/fixture/go"
    original = tree(package)
    require(original == tree(generated / "bindings/fixture/go"), "update differs from generate")
    rc("bindings", "check", *binding_flags)
    archives = root / "archives"
    rc("bindings", "package", "--output", str(archives), *binding_flags)
    require(tree(package) == original, "check or package changed committed bindings")
    with ThreadingHTTPServer(("127.0.0.1", 0), NoPublishedReleases) as server:
        server.paths = []
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            selected = dict(env, RC_BINDINGS_GITHUB_API=f"http://127.0.0.1:{server.server_address[1]}")
            plan = yaml.safe_load(rc("bindings", "plan-release", selected=selected).stdout)
            require(plan["releases"][0]["impact"] == "initial" and len(server.paths) == 1, "initial release plan differs from the local fixture")
        finally:
            server.shutdown()
            thread.join(timeout=5)

    release = yaml.safe_load((package / "runtimeconditions.binding-release.yaml").read_text())
    recorded = release["provenance"]["profiler"]
    require(recorded["kind"] == "go-module" and recorded["version"] == profiler["version"] and recorded["moduleSum"] == profiler["moduleSum"], "generated profiler identity differs from the executable")
    proxy = root / "proxy/example.test/bindings/fixture/@v"
    proxy.mkdir(parents=True)
    version = release["package"]["version"]
    asset = f"fixture-go-{version.removeprefix('v')}"
    shutil.copyfile(archives / f"{asset}.module.zip", proxy / f"{version}.zip")
    shutil.copyfile(package / "go.mod", proxy / f"{version}.mod")
    (proxy / f"{version}.info").write_text(json.dumps({"Version": version, "Time": "2026-01-01T00:00:00Z"}))
    (proxy / "list").write_text(version + "\n")
    consumer = root / "consumer"
    consumer.mkdir()
    (consumer / "go.mod").write_text(f"module example.test/consumer\n\ngo {native.removeprefix('go')}\n\nrequire example.test/bindings/fixture {version}\n")
    source = 'package main\nimport "example.test/bindings/fixture"\nvar _ = fixture.ParityService(fixture.Stream{Address: "example.test:1234"})\nfunc init(){panic("the workload must not execute")}\nfunc main(){}\n'
    (consumer / "main.go").write_text(source)
    command(["go", "mod", "download", "all"], consumer, dict(env, GOPROXY=(root / "proxy").as_uri()))
    rc("profile", "go", "validate-extension", "--dir", str(consumer), "--package", "example.test/bindings/fixture")
    flags = ("--dir", str(consumer), "--name", "ci-bindings", "--workload-uri", "https://example.test/ci", "--workload-version", "1.0.0")
    default = rc("profile", "generate", *flags)
    explicit = rc("profile", "go", "generate", *flags)
    require(default.stdout == explicit.stdout, "profile routes produced different bytes")
    profile = yaml.safe_load(default.stdout)
    require(profile["extensions"] == ["https://bindings.example.test/parity:0.1.0"] and profile["conditions"][0]["interface"] == {"type": "stream", "address": "example.test:1234"}, "generated profile differs from the fixture")

    output = root / "profile.yaml"
    output.write_text("preserve existing profile\n")
    (consumer / "main.go").write_text(source.replace('"example.test:1234"', '""'))
    for route in (("profile",), ("profile", "go")):
        failed = rc(*route, "generate", *flags, "--out", str(output), expected=1)
        require(not failed.stdout and output.read_text() == "preserve existing profile\n", "invalid declaration replaced profile output")
    changed = next(package.glob("*.go"))
    changed.write_bytes(original[changed.name] + b"\n// altered by the smoke test\n")
    rc("bindings", "check", *binding_flags, expected=1)
    require(changed.read_bytes() == original[changed.name] + b"\n// altered by the smoke test\n", "check rewrote committed source")
    changed.write_bytes(original[changed.name])
    occupied = root / "occupied"
    occupied.mkdir()
    (occupied / "keep").write_bytes(b"keep")
    rc("bindings", "generate", "--output", str(occupied), *binding_flags, expected=1)
    require(tree(occupied) == {"keep": b"keep"}, "generate overwrote a nonempty destination")
    require(tree(package) == original, "smoke checks changed the final committed tree")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prefix", type=Path, required=True)
    parser.add_argument("--fixture", type=Path, required=True)
    parser.add_argument("--core-schema", type=Path, required=True)
    parser.add_argument("--go-version", required=True)
    args = parser.parse_args()
    args.prefix, args.fixture, args.core_schema = args.prefix.resolve(), args.fixture.resolve(), args.core_schema.resolve()
    with tempfile.TemporaryDirectory(prefix="rc-cli-bindings-smoke-") as temporary:
        root = Path(temporary)
        try:
            smoke(args, root)
        finally:
            for directory, _dirs, _files in os.walk(root / "modules"):
                os.chmod(directory, 0o755)
    print("PASS: relocated CLI, all eight binding operations, generated-module profiling, and output protection")


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, ValueError, subprocess.TimeoutExpired, yaml.YAMLError) as error:
        print(f"ci-bindings-smoke: {error}", file=sys.stderr)
        sys.exit(1)
