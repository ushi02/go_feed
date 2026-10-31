package deployhook

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

var want = Claims{
	RepositoryID: "42",
	Ref:          "refs/heads/main",
	EventName:    "push",
	WorkflowRef:  "o/r/.github/workflows/ci.yml@refs/heads/main",
}

// githubClaims looks like a token GitHub would give the deploy job.
func githubClaims() map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":           Issuer,
		"aud":           Audience,
		"iat":           now.Unix(),
		"exp":           now.Add(5 * time.Minute).Unix(),
		"repository_id": want.RepositoryID,
		"ref":           want.Ref,
		"event_name":    want.EventName,
		"workflow_ref":  want.WorkflowRef,
		"sha":           testSHA,
	}
}

func newKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(claims)
	jws, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// unsigned builds a token that claims to need no signature.
func unsigned(claims map[string]any) string {
	enc := base64.RawURLEncoding.EncodeToString
	payload, _ := json.Marshal(claims)
	return enc([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + enc(payload) + "."
}

func testVerifier(key *rsa.PrivateKey) *Verifier {
	return NewVerifier(&oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{key.Public()}}, want)
}

func TestVerify(t *testing.T) {
	key := newKey(t)
	v := testVerifier(key)
	with := func(k string, val any) map[string]any {
		c := githubClaims()
		c[k] = val
		return c
	}

	sha, err := v.Verify(context.Background(), sign(t, key, githubClaims()))
	if err != nil || sha != testSHA {
		t.Fatalf("valid token: sha %q, err %v", sha, err)
	}

	rejected := []struct{ name, reason, token string }{
		{"signed by another key", `verify signature`, sign(t, newKey(t), githubClaims())},
		{"alg none", `"none"`, unsigned(githubClaims())},
		{"not a jwt", `malformed`, "hello"},
		{"other issuer", `different provider`, sign(t, key, with("iss", "https://evil.example"))},
		{"other audience", `audience`, sign(t, key, with("aud", "sts.amazonaws.com"))},
		{"expired", `expired`, sign(t, key, with("exp", time.Now().Add(-time.Minute).Unix()))},
		{"other repository", `repository_id`, sign(t, key, with("repository_id", "43"))},
		{"other branch", `ref `, sign(t, key, with("ref", "refs/heads/refactor"))},
		{"pull request", `event_name`, sign(t, key, with("event_name", "pull_request"))},
		{"other workflow", `workflow_ref`, sign(t, key, with("workflow_ref", "o/r/.github/workflows/x.yml@refs/heads/main"))},
		{"sha is a git option", `sha`, sign(t, key, with("sha", "--upload-pack=touch /tmp/x"))},
		{"sha is a branch name", `sha`, sign(t, key, with("sha", "HEAD"))},
		{"no sha", `sha`, sign(t, key, with("sha", ""))},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			sha, err := v.Verify(context.Background(), tt.token)
			if err == nil {
				t.Fatalf("accepted, sha %q", sha)
			}
			if !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("rejected for the wrong reason: %v", err)
			}
		})
	}
}
