package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildVersionExactTags(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	sh, err := exec.LookPath("sh")
	if err != nil && runtime.GOOS == "windows" {
		sh = `C:\Program Files\Git\bin\sh.exe`
		_, err = os.Stat(sh)
	}
	if err != nil {
		t.Skip("POSIX shell unavailable")
	}
	script, err := filepath.Abs("build-version.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	command := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Control API Test", "GIT_AUTHOR_EMAIL=test@example.test", "GIT_COMMITTER_NAME=Control API Test", "GIT_COMMITTER_EMAIL=test@example.test", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(dir, "empty-gitconfig"))
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, output, err)
		}
		return strings.TrimSpace(string(output))
	}
	command("init", "--quiet")
	command("-c", "commit.gpgsign=false", "commit", "--allow-empty", "--quiet", "-m", "first")
	sha := command("rev-parse", "HEAD")
	version := func(want string) {
		t.Helper()
		cmd := exec.Command(sh, filepath.ToSlash(script), "HEAD")
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(output)) != want {
			t.Fatalf("version got %s (%v), want %s", output, err, want)
		}
	}
	version("git-" + sha)
	command("tag", "v2.0.0")
	version("v2.0.0")
	command("-c", "tag.gpgsign=false", "tag", "-a", "v1.0.0", "-m", "annotated")
	version("v1.0.0")
	command("-c", "commit.gpgsign=false", "commit", "--allow-empty", "--quiet", "-m", "next")
	version("git-" + command("rev-parse", "HEAD"))
}
