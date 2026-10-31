// Package deployhook deploys the server when a GitHub Actions run on main asks
// for it. The run proves who it is with a GitHub OIDC token (a signed JWT), so
// no deploy secret is stored anywhere.
package deployhook

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	Issuer   = "https://token.actions.githubusercontent.com"
	Audience = "gugu-deploy" // the workflow asks for a token with this audience
)

// Claims are the parts of GitHub's token we check. GitHub sends them as strings.
type Claims struct {
	RepositoryID string `json:"repository_id"` // the number does not change if the repo is renamed
	Ref          string `json:"ref"`
	EventName    string `json:"event_name"`
	WorkflowRef  string `json:"workflow_ref"`
	SHA          string `json:"sha"`
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Verifier struct {
	oidc *oidc.IDTokenVerifier
	want Claims // SHA is not compared; it is the commit to deploy
}

// NewVerifier checks tokens against keys, which is GitHub's published key set
// in production and a test key in tests.
func NewVerifier(keys oidc.KeySet, want Claims) *Verifier {
	// Only RS256 is accepted (the library default), so "alg": "none" fails.
	return &Verifier{oidc: oidc.NewVerifier(Issuer, keys, &oidc.Config{ClientID: Audience}), want: want}
}

// Verify returns the commit to deploy. The error names the check that failed,
// for the log; it never contains the token.
func (v *Verifier) Verify(ctx context.Context, raw string) (string, error) {
	// Signature, issuer, audience and expiry.
	tok, err := v.oidc.Verify(ctx, raw)
	if err != nil {
		return "", err
	}
	var c Claims
	if err := tok.Claims(&c); err != nil {
		return "", err
	}
	switch {
	case c.RepositoryID != v.want.RepositoryID:
		return "", fmt.Errorf("repository_id %q", c.RepositoryID)
	case c.Ref != v.want.Ref:
		return "", fmt.Errorf("ref %q", c.Ref)
	case c.EventName != v.want.EventName:
		return "", fmt.Errorf("event_name %q", c.EventName)
	case c.WorkflowRef != v.want.WorkflowRef:
		return "", fmt.Errorf("workflow_ref %q", c.WorkflowRef)
	case !shaRe.MatchString(c.SHA):
		// The sha is passed to git, so it must be exactly a commit hash.
		return "", errors.New("sha is not a commit hash")
	}
	return c.SHA, nil
}
