class Svgcast < Formula
  desc "Turn asciinema recordings into small, crisp animated SVGs"
  homepage "https://github.com/co2water/svgcast"
  version "0.2.0"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/co2water/svgcast/releases/download/v#{version}/svgcast_#{version}_darwin_arm64.tar.gz"
      sha256 "8c972f65d6aabc274d747e9608e0477b0265f7ee4e6d47ad07d2108a7acc657a"
    end
    on_intel do
      url "https://github.com/co2water/svgcast/releases/download/v#{version}/svgcast_#{version}_darwin_amd64.tar.gz"
      sha256 "14069af9418d98325f2219d28a6061a7cd873b4d7f8fc30b8dcd7ee917563dc3"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/co2water/svgcast/releases/download/v#{version}/svgcast_#{version}_linux_arm64.tar.gz"
      sha256 "b428efbf482552fb2d92e5ef4a303ae0c74304493139f84a600086020fa71ecd"
    end
    on_intel do
      url "https://github.com/co2water/svgcast/releases/download/v#{version}/svgcast_#{version}_linux_amd64.tar.gz"
      sha256 "e618ded3828d3f7e058ea2a8261bcf3ebf192513185a4061260a5779244210ac"
    end
  end

  def install
    bin.install "svgcast"
  end

  test do
    assert_match "svgcast", shell_output("#{bin}/svgcast --version")
  end
end