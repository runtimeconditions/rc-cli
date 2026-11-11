# Release template. scripts/release-package.py replaces the version and checksums.
class RcCli < Formula
  desc "Runtime Conditions command line"
  homepage "https://github.com/runtimeconditions/rc-cli"
  version "{{VERSION}}"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/runtimeconditions/rc-cli/releases/download/v#{version}/rc-cli_#{version}_darwin_arm64.tar.gz"
      sha256 "{{DARWIN_ARM64_SHA256}}"
    end
    on_intel do
      url "https://github.com/runtimeconditions/rc-cli/releases/download/v#{version}/rc-cli_#{version}_darwin_amd64.tar.gz"
      sha256 "{{DARWIN_AMD64_SHA256}}"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/runtimeconditions/rc-cli/releases/download/v#{version}/rc-cli_#{version}_linux_arm64.tar.gz"
      sha256 "{{LINUX_ARM64_SHA256}}"
    end
    on_intel do
      url "https://github.com/runtimeconditions/rc-cli/releases/download/v#{version}/rc-cli_#{version}_linux_amd64.tar.gz"
      sha256 "{{LINUX_AMD64_SHA256}}"
    end
  end

  def install
    bin.install "bin/rc"
    prefix.install "share"
    bash_completion.install "share/bash-completion/completions/rc"
    zsh_completion.install "share/zsh/site-functions/_rc"
    fish_completion.install "share/fish/vendor_completions.d/rc.fish"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/rc --version")
    assert_match "bindings", shell_output("#{bin}/rc bindings --help")
  end
end
