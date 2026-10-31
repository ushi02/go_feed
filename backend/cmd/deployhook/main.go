// deployhook runs on the host (not in a container) as a systemd service; see
// docs/rss-reader/deploy-ec2.md, step 9. Caddy forwards /deploy requests to it.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/ushi02/go_feed/backend/internal/deployhook"
	"github.com/ushi02/go_feed/backend/internal/middleware"
)

func main() {
	// Docker's bridge address: containers can reach it, the internet cannot.
	addr := env("DEPLOYHOOK_ADDR", "172.17.0.1:9090")
	dir := env("DEPLOYHOOK_REPO_DIR", "/home/ubuntu/go_feed")

	// GitHub's public keys come from a fixed URL, not user input, so this does
	// not go through fetcher (which guards user-supplied URLs).
	ctx := oidc.ClientContext(context.Background(), &http.Client{Timeout: 10 * time.Second})
	keys := oidc.NewRemoteKeySet(ctx, deployhook.Issuer+"/.well-known/jwks")
	verifier := deployhook.NewVerifier(keys, deployhook.Claims{
		RepositoryID: "1392363315", // ushi02/go_feed
		Ref:          "refs/heads/main",
		EventName:    "push",
		WorkflowRef:  "ushi02/go_feed/.github/workflows/ci.yml@refs/heads/main",
	})
	deployer := &deployhook.Deployer{
		Dir: dir,
		Commands: [][]string{
			{"docker", "compose", "-f", "docker-compose.prod.yml", "up", "-d", "--build"},
			{"docker", "image", "prune", "-f"},
		},
		Timeout: 20 * time.Minute,
	}
	s := &deployhook.Server{Verifier: verifier, Deployer: deployer, Limiter: middleware.NewLimiter()}

	srv := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
		// Slow or oversized requests cannot hold the server.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second, // verifying may first download GitHub's keys
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
	log.Printf("deployhook listening on %s, repo %s", addr, dir)
	log.Fatal(srv.ListenAndServe())
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
