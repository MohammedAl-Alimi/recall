# Homebrew formula template for recall.
#
# goreleaser overwrites this file on every tagged release with a bottle-less
# formula that downloads the prebuilt archive (see .goreleaser.yaml, brews).
# Until the first release, the formula builds from source with `--HEAD`:
#
#   brew install --HEAD MohammedAl-Alimi/tap/recall
#
class Recall < Formula
  desc "Terminal session manager for the Claude Code CLI"
  homepage "https://github.com/MohammedAl-Alimi/recall"
  license "MIT"
  head "https://github.com/MohammedAl-Alimi/recall.git", branch: "main"

  depends_on "go" => :build

  def install
    ENV["CGO_ENABLED"] = "0"
    ldflags = "-s -w -X main.version=#{version}"
    system "go", "build", *std_go_args(ldflags: ldflags), "./cmd/recall"
  end

  def caveats
    <<~EOS
      Run `recall doctor` to check the environment, then `recall setup` to
      raise Claude's transcript retention and install the Ctrl-G widget.
      Each setup step asks before changing anything.
    EOS
  end

  test do
    assert_match "recall", shell_output("#{bin}/recall version")
    assert_match "not yet", shell_output("#{bin}/recall index")
  end
end
