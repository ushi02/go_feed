package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Search sends the query with the key to /v1/tools/search and reads the results.
func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/tools/search" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-secret" {
			t.Errorf("Authorization = %q", got)
		}
		var body struct {
			Query string `json:"query"`
			Count int    `json:"count"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Query != "日本 求职" || body.Count != 5 {
			t.Errorf("body %+v, err %v", body, err)
		}
		w.Write([]byte(`{"results":[{"title":"在日本找工作","url":"https://blog.example/jp-job","site_name":"某博客","date":"2026-05-01T00:00:00Z","summary":"经验","rerank_score":2.1}]}`))
	}))
	defer srv.Close()

	got, err := testClient(srv.URL).Search(context.Background(), "日本 求职", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "https://blog.example/jp-job" || got[0].Title != "在日本找工作" || got[0].Summary != "经验" {
		t.Fatalf("results: %+v", got)
	}
}

// An error status, e.g. 402 when the balance ran out, is an error that says so.
func TestSearchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"error":{"message":"Insufficient Balance"}}`))
	}))
	defer srv.Close()

	_, err := testClient(srv.URL).Search(context.Background(), "go", 5)
	if err == nil || !strings.Contains(err.Error(), "402") {
		t.Fatalf("err = %v, want HTTP 402", err)
	}
}
