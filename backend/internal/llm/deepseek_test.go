package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func testClient(url string) *DeepSeek {
	return &DeepSeek{apiKey: "sk-secret", model: "deepseek-flash", baseURL: url, http: &http.Client{Timeout: 5 * time.Second}}
}

var searchTool = ToolDef{Name: "search", Description: "Search entries", Parameters: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)}

func TestDeepSeekRequestAndToolCallResponse(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-secret" {
			t.Errorf("unexpected request %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		// A streamed tool call: ID and name first, arguments split over two chunks.
		io.WriteString(w, `data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":""}}]}}]}

: keep-alive

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"go\"}"}}]},"finish_reason":"tool_calls"}]}

data: [DONE]

`)
	}))
	defer srv.Close()

	history := []Message{
		{Role: RoleUser, Content: "找 Go 博客"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_0", Type: "function", Function: FunctionCall{Name: "search", Arguments: `{"q":"golang"}`}}}},
		{Role: RoleTool, ToolCallID: "call_0", Content: "[]"},
	}
	msg, err := testClient(srv.URL).Chat(context.Background(), Request{Messages: history, Tools: []ToolDef{searchTool}, MaxTokens: 800}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call_1" || msg.ToolCalls[0].Function.Arguments != `{"q":"go"}` {
		t.Fatalf("tool call not parsed: %+v", msg)
	}

	if got["model"] != "deepseek-flash" || got["stream"] != true || got["max_tokens"] != 800.0 {
		t.Errorf("model/stream/max_tokens: %v %v %v", got["model"], got["stream"], got["max_tokens"])
	}
	if th, _ := got["thinking"].(map[string]any); th["type"] != "disabled" {
		t.Errorf("thinking not disabled: %v", got["thinking"])
	}
	tool := got["tools"].([]any)[0].(map[string]any)
	if fn := tool["function"].(map[string]any); tool["type"] != "function" || fn["name"] != "search" || fn["parameters"] == nil {
		t.Errorf("tool not serialized: %v", tool)
	}
	msgs := got["messages"].([]any)
	if a := msgs[1].(map[string]any); a["tool_calls"] == nil {
		t.Errorf("assistant tool_calls not sent back: %v", a)
	}
	if tr := msgs[2].(map[string]any); tr["role"] != "tool" || tr["tool_call_id"] != "call_0" {
		t.Errorf("tool result not linked to its call: %v", tr)
	}
	if u := msgs[0].(map[string]any); u["tool_calls"] != nil || u["tool_call_id"] != nil {
		t.Errorf("empty tool fields should be omitted: %v", u)
	}
}

func TestDeepSeekErrorDoesNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"error":{"message":"Authentication Fails"}}`)
	}))
	defer srv.Close()
	_, err := testClient(srv.URL).Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "sk-secret") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Text arrives piece by piece and each piece is passed on as it comes.
func TestDeepSeekStreamsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"咕，\"}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	var pieces []string
	msg, err := testClient(srv.URL).Chat(context.Background(), Request{}, func(s string) { pieces = append(pieces, s) })
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "咕，你好" || msg.Role != RoleAssistant || strings.Join(pieces, "|") != "咕，|你好" {
		t.Fatalf("msg %+v, pieces %q", msg, pieces)
	}
}

// A stream that breaks off returns the text so far with an error, and drops
// tool calls, whose arguments may be cut off.
func TestDeepSeekBrokenStreamKeepsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"前半段\",\"tool_calls\":[{\"index\":0,\"id\":\"c\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"q\"}}]}}]}\n\n")
	}))
	defer srv.Close()
	msg, err := testClient(srv.URL).Chat(context.Background(), Request{}, nil)
	if err == nil || msg.Content != "前半段" || len(msg.ToolCalls) != 0 {
		t.Fatalf("msg %+v, err %v", msg, err)
	}
}

func TestDeepSeekFromEnvNeedsKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	if _, err := NewDeepSeekFromEnv(); err == nil {
		t.Fatal("expected an error without DEEPSEEK_API_KEY")
	}
}

// TestDeepSeekLive calls the real API. It runs only when DEEPSEEK_API_KEY is
// set, so CI and normal test runs never send anything to DeepSeek:
//
//	DEEPSEEK_API_KEY=sk-... go test ./internal/llm/ -run Live -v
func TestDeepSeekLive(t *testing.T) {
	if os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}
	client, err := NewDeepSeekFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	msg, err := client.Chat(ctx, Request{
		Messages: []Message{{Role: RoleUser, Content: "用 search 工具搜一下关于 Go 的文章。"}},
		Tools:    []ToolDef{searchTool},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reply: %+v", msg)
	if len(msg.ToolCalls) == 0 {
		t.Fatal("expected the model to call the search tool")
	}
}
