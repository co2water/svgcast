# Homebrew formula。
#
# 位置：packaging/homebrew/——dist/ 是 goreleaser 的輸出目錄，被 .gitignore 忽略，
# 放那裡會導致 formula 永遠不進 repo（第一次就踩到了）。
#
# 放進你自己的 tap（例如 co2water/homebrew-tap），使用者就能：
#   brew install co2water/tap/svgcast
#
# ⚠️ version 與 sha256 每次發版都要更新。goreleaser 可以自動維護這個檔案
# （在 .goreleaser.yaml 加 brews: 區塊），等 tap repo 建好再開啟那段。
class Svgcast < Formula
  desc "Turn asciinema recordings into small, crisp animated SVGs"
  homepage "https://github.com/co2water/svgcast"
  version "0.1.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/co2water/svgcast/releases/download/v#{version}/svgcast_#{version}_darwin_arm64.tar.gz"
      sha256 "REPLACE_WITH_CHECKSUM"
    end
    on_intel do
      url "https://github.com/co2water/svgcast/releases/download/v#{version}/svgcast_#{version}_darwin_amd64.tar.gz"
      sha256 "REPLACE_WITH_CHECKSUM"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/co2water/svgcast/releases/download/v#{version}/svgcast_#{version}_linux_arm64.tar.gz"
      sha256 "REPLACE_WITH_CHECKSUM"
    end
    on_intel do
      url "https://github.com/co2water/svgcast/releases/download/v#{version}/svgcast_#{version}_linux_amd64.tar.gz"
      sha256 "REPLACE_WITH_CHECKSUM"
    end
  end

  def install
    bin.install "svgcast"
  end

  test do
    assert_match "svgcast", shell_output("#{bin}/svgcast --version")
  end
end
