# Homebrew formula for Matthew-Eucaristo/homebrew-tap (Formula/fell.rb).
# Builds from the tagged source tarball. Bump `url` and `sha256` per release;
# see RELEASING.md.
class Fell < Formula
  desc "Find and safely clean up git worktrees left behind by AI coding agents"
  homepage "https://github.com/Matthew-Eucaristo/fell"
  url "https://github.com/Matthew-Eucaristo/fell/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "REPLACE_WITH_SHA256_OF_THE_TAG_TARBALL"
  license "MIT"
  head "https://github.com/Matthew-Eucaristo/fell.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-X main.version=#{version}")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/fell --version")
    assert_match "\"schema_version\": 1", shell_output("#{bin}/fell capabilities --json")

    ENV["GIT_AUTHOR_NAME"] = ENV["GIT_COMMITTER_NAME"] = "test"
    ENV["GIT_AUTHOR_EMAIL"] = ENV["GIT_COMMITTER_EMAIL"] = "test@example.com"
    system "git", "init", "-q", "-b", "main", testpath/"app"
    system "git", "-C", testpath/"app", "commit", "-q", "--allow-empty", "-m", "init"
    system "git", "-C", testpath/"app", "worktree", "add", "-q", "--detach", testpath/"app-wt"
    output = shell_output("#{bin}/fell scan --json --no-agents #{testpath}")
    assert_match "app-wt", output
    assert_path_exists testpath/"app-wt" # scan never changes anything
  end
end
