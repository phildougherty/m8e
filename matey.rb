class Matey < Formula
  desc "Kubernetes-native MCP server and AI workflow orchestrator"
  homepage "https://github.com/phildougherty/m8e"
  url "https://github.com/phildougherty/m8e/archive/refs/tags/v0.0.4.tar.gz"
  sha256 "SHA256_PLACEHOLDER"
  license "Apache-2.0"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w"), "./cmd/matey"
  end

  test do
    system "#{bin}/matey", "--version"
  end
end