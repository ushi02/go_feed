package deployhook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit adds an empty commit in dir and returns its hash.
func commit(t *testing.T, dir, msg string) string {
	git(t, dir, "commit", "--allow-empty", "-q", "-m", msg)
	return git(t, dir, "rev-parse", "HEAD")
}

// setupRepos makes an origin with main = a, b and a side branch c, and a
// server checkout that has deployed a.
func setupRepos(t *testing.T) (server, a, b, c string) {
	// Ignore the developer's own git config (signing, hooks, default branch).
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "test")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "test@example.com")
	}
	root := t.TempDir()
	origin, dev, server := filepath.Join(root, "origin"), filepath.Join(root, "dev"), filepath.Join(root, "server")
	git(t, root, "init", "-q", "--bare", "-b", "main", origin)
	git(t, root, "clone", "-q", origin, dev)
	a = commit(t, dev, "a")
	git(t, dev, "push", "-q", "origin", "HEAD:main")
	git(t, root, "clone", "-q", origin, server)

	b = commit(t, dev, "b")
	git(t, dev, "push", "-q", "origin", "HEAD:main")
	git(t, dev, "checkout", "-q", "-b", "side")
	c = commit(t, dev, "c")
	git(t, dev, "push", "-q", "origin", "side")
	git(t, dev, "checkout", "-q", "main")
	return server, a, b, c
}

func TestDeploy(t *testing.T) {
	server, a, b, c := setupRepos(t)
	marker := filepath.Join(t.TempDir(), "built")
	d := &Deployer{Dir: server, Commands: [][]string{{"touch", marker}}, Timeout: time.Minute}
	head := func() string { return git(t, server, "rev-parse", "HEAD") }

	if err := d.deploy(c); err == nil || head() != a {
		t.Fatalf("commit not on main: err %v, head %s", err, head())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("ran the build for a rejected commit")
	}

	if err := d.deploy(b); err != nil || head() != b {
		t.Fatalf("newer commit: err %v, head %s", err, head())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("build command did not run")
	}

	// A replayed old token must not roll the site back.
	if err := d.deploy(a); err == nil || head() != b {
		t.Fatalf("older commit: err %v, head %s", err, head())
	}
	// Running the same commit again is allowed (e.g. after a failed build).
	if err := d.deploy(b); err != nil {
		t.Fatalf("same commit: %v", err)
	}
}

func TestDeployOnlyTestedCommit(t *testing.T) {
	server, _, b, _ := setupRepos(t)
	// Main moves on to d while b's deploy is about to start: b is what CI
	// tested, so b is what gets deployed.
	dev := filepath.Join(filepath.Dir(server), "dev")
	commit(t, dev, "d")
	git(t, dev, "push", "-q", "origin", "HEAD:main")

	d := &Deployer{Dir: server, Timeout: time.Minute}
	if err := d.deploy(b); err != nil {
		t.Fatal(err)
	}
	if got := git(t, server, "rev-parse", "HEAD"); got != b {
		t.Fatalf("deployed %s, want %s", got, b)
	}
}

func TestDeployFailsOnBuildError(t *testing.T) {
	server, _, b, _ := setupRepos(t)
	d := &Deployer{Dir: server, Commands: [][]string{{"false"}}, Timeout: time.Minute}
	if !d.Start(b) {
		t.Fatal("Start refused")
	}
	deadline := time.Now().Add(10 * time.Second)
	for d.Status().State == "running" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s := d.Status(); s.State != "failed" || s.SHA != b {
		t.Fatalf("status %+v", s)
	}
}
