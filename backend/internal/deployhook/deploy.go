package deployhook

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Status is what GET /deploy/status returns. It holds no command output, which
// stays in the server log.
type Status struct {
	State string `json:"state"` // idle, running, succeeded or failed
	SHA   string `json:"sha,omitempty"`
	Error string `json:"error,omitempty"`
}

// Deployer runs one deploy at a time in the repo checkout at Dir.
type Deployer struct {
	Dir string
	// Run after the checkout moves to the new commit, e.g. docker compose up --build.
	Commands [][]string
	Timeout  time.Duration

	mu     sync.Mutex
	status Status
}

func (d *Deployer) Status() Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.State == "" {
		return Status{State: "idle"}
	}
	return d.status
}

// Start deploys sha in the background, so a dropped connection (for example
// while Caddy restarts) does not stop it. It returns false if a deploy is running.
func (d *Deployer) Start(sha string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status.State == "running" {
		return false
	}
	d.status = Status{State: "running", SHA: sha}
	go d.finish(sha)
	return true
}

func (d *Deployer) finish(sha string) {
	err := d.deploy(sha)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		log.Printf("deployhook: deploy %s failed: %v", sha, err)
		d.status = Status{State: "failed", SHA: sha, Error: err.Error()}
		return
	}
	log.Printf("deployhook: deployed %s", sha)
	d.status = Status{State: "succeeded", SHA: sha}
}

func (d *Deployer) deploy(sha string) error {
	ctx, cancel := context.WithTimeout(context.Background(), d.Timeout)
	defer cancel()
	if err := d.run(ctx, "git", "fetch", "--quiet", "origin", "main"); err != nil {
		return fmt.Errorf("git fetch: %w", err)
	}
	// Deploy exactly the commit CI tested, not whatever main points to now:
	// a newer push may still be in CI.
	if d.run(ctx, "git", "merge-base", "--is-ancestor", sha, "origin/main") != nil {
		return fmt.Errorf("commit is not on main")
	}
	// Refuse to go back: a replayed old token must not roll the site back.
	if d.run(ctx, "git", "merge-base", "--is-ancestor", "HEAD", sha) != nil {
		return fmt.Errorf("commit is older than the deployed one")
	}
	if err := d.run(ctx, "git", "merge", "--ff-only", "--quiet", sha); err != nil {
		return fmt.Errorf("git merge: %w", err)
	}
	for _, c := range d.Commands {
		if err := d.run(ctx, c[0], c[1:]...); err != nil {
			return fmt.Errorf("%s: %w", c[0], err)
		}
	}
	return nil
}

// run executes a fixed program with separate arguments; nothing goes through a shell.
func (d *Deployer) run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = d.Dir
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr // journald keeps it
	return cmd.Run()
}
