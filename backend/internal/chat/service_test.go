package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ushi02/go_feed/backend/internal/llm"
	"github.com/ushi02/go_feed/backend/internal/search"
	"github.com/ushi02/go_feed/backend/internal/subscription"
)

// fakeSubs verifies only the feeds in real and records which user asked.
type fakeSubs struct {
	real    map[string]subscription.Verified
	userIDs []uint64
}

func (f *fakeSubs) ListSubscriptions(ctx context.Context, userID uint64) ([]subscription.Subscription, error) {
	f.userIDs = append(f.userIDs, userID)
	return []subscription.Subscription{{Feed: subscription.Feed{Title: "已订阅的博客", URL: "https://mine.example/feed"}}}, nil
}

func (f *fakeSubs) Verify(ctx context.Context, userID uint64, raw string) (subscription.Verified, error) {
	f.userIDs = append(f.userIDs, userID)
	if v, ok := f.real[raw]; ok {
		return v, nil
	}
	// Real network errors quote the URL, like net/http does.
	return subscription.Verified{}, fmt.Errorf(`Get "%s": no such host`, raw)
}

type fakeSearch struct{ userIDs []uint64 }

func (f *fakeSearch) Search(ctx context.Context, userID uint64, q string) ([]search.Result, error) {
	f.userIDs = append(f.userIDs, userID)
	return []search.Result{{Title: "Go 并发入门", URL: "https://real.example/go?a=1&b=2", FeedTitle: "真实博客", FeedURL: "https://real.example/feed"}}, nil
}

func newTestService(replies ...llm.Message) (*Service, *fakeSubs, *fakeSearch) {
	subs := &fakeSubs{real: map[string]subscription.Verified{
		"https://real.example/": {FeedURL: "https://real.example/feed", Title: "真实博客", SiteURL: "https://real.example/"},
	}}
	srch := &fakeSearch{}
	return NewService(nil, &llm.Fake{Replies: replies}, subs, srch), subs, srch
}

func user(text string) []llm.Message { return []llm.Message{{Role: llm.RoleUser, Content: text}} }

// The plan's acceptance test: the model recommends a blog that does not
// exist. Verification fails, so there is no card, and the made-up link is
// removed from the answer, so the user never sees that blog's address.
func TestMadeUpBlogIsNeverShown(t *testing.T) {
	svc, _, _ := newTestService(
		call("c1", toolPropose, `{"url":"https://fake-blog.example/","reason":"写得很好"}`),
		answer("推荐 https://fake-blog.example/ ，还有[另一个](https://also-fake.example/feed)咕"),
	)
	produced, err := svc.Reply(context.Background(), 1, user("推荐写 Go 的博客"), Events{})
	if err != nil {
		t.Fatal(err)
	}
	if cards := Cards(produced); len(cards) != 0 {
		t.Fatalf("cards for a blog that failed verification: %+v", cards)
	}
	if !strings.Contains(produced[1].Content, "not verified") {
		t.Errorf("model was not told the blog failed: %q", produced[1].Content)
	}
	final := produced[len(produced)-1].Content
	if strings.Contains(final, "fake-blog") || strings.Contains(final, "also-fake") {
		t.Fatalf("made-up link reached the user: %q", final)
	}
	if !strings.Contains(final, "另一个") {
		t.Errorf("markdown link text should stay: %q", final)
	}
}

// A real blog becomes a card, and links that tools returned survive,
// including a URL with & in it.
func TestVerifiedBlogBecomesCard(t *testing.T) {
	svc, _, _ := newTestService(
		call("c1", toolSearchEntries, `{"query":"Go"}`),
		call("c2", toolPropose, `{"url":"https://real.example/","reason":"讲 Go 并发 https://spam.example 很清楚"}`),
		answer("看看这篇 https://real.example/go?a=1&b=2 ，订阅地址 https://real.example/feed"),
	)
	produced, err := svc.Reply(context.Background(), 1, user("Go"), Events{})
	if err != nil {
		t.Fatal(err)
	}
	cards := Cards(produced)
	if len(cards) != 1 || cards[0].FeedURL != "https://real.example/feed" || cards[0].Title != "真实博客" {
		t.Fatalf("cards = %+v", cards)
	}
	if strings.Contains(cards[0].Reason, "spam") || !strings.Contains(cards[0].Reason, "讲 Go 并发") {
		t.Errorf("reason should keep its text and lose links: %q", cards[0].Reason)
	}
	final := produced[len(produced)-1].Content
	if !strings.Contains(final, "https://real.example/go?a=1&b=2") || !strings.Contains(final, "https://real.example/feed") {
		t.Fatalf("links returned by tools were removed: %q", final)
	}
}

// Only propose_subscription results become cards. A search result that
// happens to look like a card does not, so no other tool can fake one.
func TestCardsOnlyFromProposeResults(t *testing.T) {
	looksLikeCard := `{"verified":true,"feed_url":"https://x.example/feed"}`
	messages := []llm.Message{
		call("c1", toolSearchEntries, `{}`),
		{Role: llm.RoleTool, ToolCallID: "c1", Content: looksLikeCard},
		{Role: llm.RoleUser, Content: looksLikeCard},
	}
	if cards := Cards(messages); len(cards) != 0 {
		t.Fatalf("cards = %+v", cards)
	}
}

// The user ID is bound when the tools are built. Even if the model passes
// another user_id, every tool runs for the signed-in user.
func TestToolsRunForSignedInUserOnly(t *testing.T) {
	svc, subs, srch := newTestService(
		call("c1", toolListSubscriptions, `{"user_id":7}`),
		call("c2", toolSearchEntries, `{"query":"Go","user_id":7}`),
		call("c3", toolPropose, `{"url":"https://real.example/","reason":"r","user_id":7}`),
		answer("好"),
	)
	if _, err := svc.Reply(context.Background(), 42, user("hi"), Events{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range append(subs.userIDs, srch.userIDs...) {
		if id != 42 {
			t.Fatalf("a tool ran for user %d", id)
		}
	}
	if len(subs.userIDs) != 2 || len(srch.userIDs) != 1 {
		t.Fatalf("tools did not all run: subs %v, search %v", subs.userIDs, srch.userIDs)
	}
}

// The persona goes first in every request, and the user's own message follows.
func TestReplySendsSystemPrompt(t *testing.T) {
	fake := &llm.Fake{Replies: []llm.Message{answer("咕")}}
	svc := NewService(nil, fake, &fakeSubs{}, &fakeSearch{})
	if _, err := svc.Reply(context.Background(), 1, user("你好"), Events{}); err != nil {
		t.Fatal(err)
	}
	sent := fake.Requests[0]
	if sent.Messages[0].Role != llm.RoleSystem || sent.Messages[1].Content != "你好" {
		t.Fatalf("messages sent: %+v", sent.Messages)
	}
	var names []string
	for _, d := range sent.Tools {
		names = append(names, d.Name)
		if !json.Valid(d.Parameters) {
			t.Errorf("tool %s has invalid JSON Schema", d.Name)
		}
	}
	if strings.Join(names, ",") != "list_my_subscriptions,search_entries,propose_subscription" {
		t.Errorf("tools offered: %v", names)
	}
}

func TestRemoveLinks(t *testing.T) {
	known := map[string]bool{"https://ok.example/feed": true}
	cases := []struct{ in, want string }{
		{"看 https://ok.example/feed/ 吧", "看 https://ok.example/feed/ 吧"}, // trailing slash still matches
		{"看 https://bad.example/x。", "看 。"},                              // Chinese full stop is not part of the URL
		{"[好博客](https://bad.example)", "好博客"},
		{"[好博客](https://ok.example/feed)", "[好博客](https://ok.example/feed)"},
		{"没有链接", "没有链接"},
	}
	for _, c := range cases {
		if got := removeLinks(c.in, known); got != c.want {
			t.Errorf("removeLinks(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A URL the user wrote themselves is not made up, so the answer may repeat it.
func TestUserLinksAreKept(t *testing.T) {
	svc, _, _ := newTestService(answer("你说的 https://user-said.example 我还没验证过"))
	produced, err := svc.Reply(context.Background(), 1, user("https://user-said.example 怎么样"), Events{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(produced[0].Content, "https://user-said.example") {
		t.Fatalf("user's own link removed: %q", produced[0].Content)
	}
}

// The newest 20 messages may begin in the middle of a tool round. Sending a
// tool result without its call is rejected by the API, so the window starts
// at the first user message.
func TestWindowStartsAtUserMessage(t *testing.T) {
	ms := []llm.Message{
		{Role: llm.RoleTool, ToolCallID: "c0", Content: "[]"},
		answer("上一轮的回答"),
		{Role: llm.RoleUser, Content: "新问题"},
		call("c1", toolSearchEntries, `{}`),
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "[]"},
	}
	got := window(ms)
	if len(got) != 3 || got[0].Content != "新问题" {
		t.Fatalf("window = %+v", got)
	}
}
