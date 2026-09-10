# Homebrew formula for recall. Published in MohammedAl-Alimi/homebrew-tap.
class Recall < Formula
  desc "Every Claude Code terminal session, listed, searchable, and one keypress away"
  homepage "https://recall-phi-pearl.vercel.app"
  version "0.1.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/MohammedAl-Alimi/recall/releases/download/v0.1.0/recall_0.1.0_darwin_arm64.tar.gz"
      sha256 "088fdaa870426cdb8afc9fa51a869bcf9469b8c2c1c58a3b039d21a302ae52ad"
    else
      url "https://github.com/MohammedAl-Alimi/recall/releases/download/v0.1.0/recall_0.1.0_darwin_amd64.tar.gz"
      sha256 "c341708565c04d4780402c56d9d677afbd63327d5f4480b9fdd01067dcbf146c"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/MohammedAl-Alimi/recall/releases/download/v0.1.0/recall_0.1.0_linux_arm64.tar.gz"
      sha256 "1bed7be9927d9520a5d0a9d5aec1e434da860b2a743d5c0000298f82930079f8"
    else
      url "https://github.com/MohammedAl-Alimi/recall/releases/download/v0.1.0/recall_0.1.0_linux_amd64.tar.gz"
      sha256 "320519dfab0461cae5f1f78250d87ffc2d4dde7b227162cf5f64ed218d33a57e"
    end
  end

  def install
    bin.install "recall"
  end

  def caveats
    <<~EOS
      recall never changes ~/.claude on its own. To stop Claude Code deleting
      sessions after 30 days and to add the Ctrl-G shortcut, run:
        recall setup
      Remove everything it added with: recall uninstall
    EOS
  end

  test do
    assert_match "recall", shell_output("#{bin}/recall version")
  end
end
