package deployhook

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ushi02/go_feed/backend/internal/middleware"
)

// Each request may make the verifier download GitHub's keys again (it does so
// whenever a signature fails), so requests are limited per client IP.
// A real deploy sends one POST and a status check every 10 seconds.
const perMinute = 20

type Server struct {
	Verifier *Verifier
	Deployer *Deployer
	Limiter  *middleware.Limiter
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /deploy", s.deploy)
	mux.HandleFunc("GET /deploy/status", s.status)
	return mux
}

func (s *Server) deploy(w http.ResponseWriter, r *http.Request) {
	sha, ok := s.check(w, r)
	if !ok {
		return
	}
	if !s.Deployer.Start(sha) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a deploy is already running"})
		return
	}
	writeJSON(w, http.StatusAccepted, s.Deployer.Status())
}

// The repo is private, so even the status needs a valid token.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.check(w, r); ok {
		writeJSON(w, http.StatusOK, s.Deployer.Status())
	}
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !s.Limiter.Allow("deploy:"+clientIP(r), perMinute, time.Minute) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests"})
		return "", false
	}
	// The token travels in a header, not the URL: URLs end up in logs.
	raw, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing token"})
		return "", false
	}
	sha, err := s.Verifier.Verify(r.Context(), raw)
	if err != nil {
		log.Printf("deployhook: rejected token: %v", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return "", false
	}
	return sha, true
}

// clientIP trusts X-Forwarded-For because only Caddy can reach this server
// from outside, and Caddy replaces the header with the real client address.
func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
