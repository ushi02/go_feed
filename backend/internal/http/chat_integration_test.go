package http_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apihttp "github.com/ushi02/go_feed/backend/internal/http"
	"github.com/ushi02/go_feed/backend/internal/llm"
)

type chatItem struct {
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	Incomplete bool   `json:"incomplete"`
	Card       *struct {
		FeedURL string `json:"feed_url"`
		Title   string `json:"title"`
	} `json:"card"`
}

type sseEvent struct{ name, data string }

func newChatClient(t *testing.T, username string, model llm.Client) *client {
	c := &client{t: t, router: apihttp.NewRouter(database, model)}
	c.do("POST", "/api/auth/register", map[string]string{"username": username, "password": "password123"}, 201, nil)
	return c
}

func (c *client) chatItems() []chatItem {
	var out struct {
		Items []chatItem `json:"items"`
	}
	c.do("GET", "/api/chat", nil, 200, &out)
	return out.Items
}

// sendChat posts a message and returns the server-sent events of the reply.
func (c *client) sendChat(text string) []sseEvent {
	c.t.Helper()
	body, _ := json.Marshal(map[string]string{"text": text})
	req := httptest.NewRequest("POST", "/api/chat/messages", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	req.AddCookie(c.cookie)
	rec := httptest.NewRecorder()
	c.router.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/event-stream" {
		c.t.Fatalf("send chat = %d %s: %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	var events []sseEvent
	for _, block := range strings.Split(strings.TrimSpace(rec.Body.String()), "\n\n") {
		var e sseEvent
		for _, line := range strings.Split(block, "\n") {
			if v, ok := strings.CutPrefix(line, "event: "); ok {
				e.name = v
			} else if v, ok := strings.CutPrefix(line, "data: "); ok {
				e.data = v
			}
		}
		events = append(events, e)
	}
	return events
}

func call(id, name, args string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: args}}}}
}

func TestChatConversationIsStored(t *testing.T) {
	skipShort(t)
	feed := newFeedServer(t, item(1))
	fake := &llm.Fake{Replies: []llm.Message{
		call("c1", "propose_subscription", `{"url":"`+feed.URL()+`","reason":"写得很认真"}`),
		{Role: llm.RoleAssistant, Content: "找到一个，咕"},
	}}
	alice := newChatClient(t, "chat-alice", fake)

	events := alice.sendChat("推荐个博客")
	var names []string
	var text strings.Builder
	for _, e := range events {
		names = append(names, e.name)
		if e.name == "text" {
			var d struct{ Text string }
			json.Unmarshal([]byte(e.data), &d)
			text.WriteString(d.Text)
		}
	}
	if names[0] != "card" || names[len(names)-1] != "done" || text.String() != "找到一个，咕" {
		t.Fatalf("events %v, text %q", names, text.String())
	}

	// A page refresh loads the conversation back: the question, the card, the answer.
	items := alice.chatItems()
	if len(items) != 3 || items[0].Text != "推荐个博客" || items[1].Kind != "card" ||
		items[1].Card.FeedURL != feed.URL() || items[2].Kind != "assistant" || items[2].Text != "找到一个，咕" {
		t.Fatalf("items after refresh: %+v", items)
	}

	if other := newChatClient(t, "chat-bob", nil).chatItems(); len(other) != 0 {
		t.Fatalf("bob sees alice's chat: %+v", other)
	}

	alice.do("POST", "/api/chat/conversations", nil, 204, nil)
	if items := alice.chatItems(); len(items) != 0 {
		t.Fatalf("new conversation is not empty: %+v", items)
	}
}

// Only the newest 20 messages go to the model, however long the chat gets.
func TestChatSendsOnlyRecentHistory(t *testing.T) {
	skipShort(t)
	fake := &llm.Fake{}
	for range 15 {
		fake.Replies = append(fake.Replies, llm.Message{Role: llm.RoleAssistant, Content: "好的"})
	}
	c := newChatClient(t, "chat-long", fake)
	for range 15 {
		c.sendChat("再聊一句")
	}
	// At the 15th send 29 messages are stored. The newest 20 begin with an
	// answer, which is dropped so the history starts with a question: 19 remain.
	last := fake.Requests[len(fake.Requests)-1].Messages
	if len(last) != 1+19 || last[0].Role != llm.RoleSystem || last[1].Role != llm.RoleUser {
		t.Fatalf("last request has %d messages, first after system: %+v", len(last), last[1])
	}
	if items := c.chatItems(); len(items) != 30 {
		t.Fatalf("page shows %d items, want all 30", len(items))
	}
}

// slowLLM sends half an answer, then waits until the request is cancelled.
type slowLLM struct{}

func (slowLLM) Chat(ctx context.Context, req llm.Request, onText func(string)) (llm.Message, error) {
	onText("前半段，")
	<-ctx.Done()
	return llm.Message{Role: llm.RoleAssistant, Content: "前半段，"}, ctx.Err()
}

// Closing the page mid-reply cancels the request. The text generated so far
// is still saved, marked incomplete.
func TestChatSavesPartialReplyWhenPageCloses(t *testing.T) {
	skipShort(t)
	c := newChatClient(t, "chat-closer", slowLLM{})
	srv := httptest.NewServer(c.router)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/api/chat/messages", strings.NewReader(`{"text":"讲个长故事"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", srv.URL)
	req.AddCookie(c.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() && !strings.Contains(sc.Text(), "前半段") {
	}
	cancel() // the user closes the page
	resp.Body.Close()

	deadline := time.Now().Add(5 * time.Second)
	for {
		items := c.chatItems()
		if len(items) == 2 && items[1].Text == "前半段，" && items[1].Incomplete {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("partial reply not saved: %+v", items)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestChatErrors(t *testing.T) {
	skipShort(t)
	off := newChatClient(t, "chat-off", nil)
	off.do("POST", "/api/chat/messages", map[string]string{"text": "你好"}, 503, nil)

	on := newChatClient(t, "chat-on", &llm.Fake{})
	on.do("POST", "/api/chat/messages", map[string]string{"text": "   "}, 400, nil)
	on.do("POST", "/api/chat/messages", map[string]string{"text": strings.Repeat("长", 1001)}, 400, nil)
	// The model failing before any text: a JSON error, and the question is kept.
	on.do("POST", "/api/chat/messages", map[string]string{"text": "你好"}, 502, nil)
	if items := on.chatItems(); len(items) != 1 || items[0].Text != "你好" {
		t.Fatalf("items: %+v", items)
	}
}
