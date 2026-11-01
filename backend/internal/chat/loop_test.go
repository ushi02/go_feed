package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ushi02/go_feed/backend/internal/llm"
)

func call(id, name, args string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Type: "function", Function: llm.FunctionCall{Name: name, Arguments: args}}}}
}

func answer(text string) llm.Message { return llm.Message{Role: llm.RoleAssistant, Content: text} }

// searchTool records the query it was called with.
func searchTool(got *string) Tool {
	return Tool{
		Def: llm.ToolDef{Name: "search", Parameters: json.RawMessage(`{"type":"object"}`)},
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct{ Q string }
			if err := json.Unmarshal(args, &a); err != nil {
				return "", err
			}
			*got = a.Q
			return `[{"title":"Go 并发入门"}]`, nil
		},
	}
}

// One full round trip: the model asks for a tool, gets the result, then answers.
func TestRunToolsOneRound(t *testing.T) {
	var query string
	fake := &llm.Fake{Replies: []llm.Message{call("call_1", "search", `{"q":"golang"}`), answer("推荐《Go 并发入门》")}}
	history := []llm.Message{{Role: llm.RoleUser, Content: "推荐 Go 的文章"}}

	produced, err := RunTools(context.Background(), fake, history, []Tool{searchTool(&query)}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if query != "golang" {
		t.Errorf("tool got query %q", query)
	}
	// Every message is returned so the caller can store tool calls as they were.
	if len(produced) != 3 || len(produced[0].ToolCalls) != 1 || produced[1].Role != llm.RoleTool ||
		produced[1].ToolCallID != "call_1" || produced[2].Content != "推荐《Go 并发入门》" {
		t.Fatalf("produced messages: %+v", produced)
	}
	// The second request carries the tool call and its result back to the model.
	second := fake.Requests[1].Messages
	if len(fake.Requests) != 2 || len(second) != 3 || second[2].Content != `[{"title":"Go 并发入门"}]` {
		t.Fatalf("second request: %+v", second)
	}
	if len(fake.Requests[0].Tools) != 1 || fake.Requests[0].Tools[0].Name != "search" {
		t.Errorf("tools not offered to the model: %+v", fake.Requests[0].Tools)
	}
}

// Mistakes by the model or a tool go back to the model as the tool result,
// so it can correct itself; they do not end the conversation.
func TestRunToolsReportsErrorsToModel(t *testing.T) {
	failing := Tool{Def: llm.ToolDef{Name: "broken"}, Run: func(context.Context, json.RawMessage) (string, error) {
		return "", errors.New("database is down")
	}}
	var query string
	fake := &llm.Fake{Replies: []llm.Message{
		call("c1", "no_such_tool", `{}`),
		call("c2", "search", `{"q": not json`),
		call("c3", "broken", `{}`),
		answer("抱歉，暂时查不到"),
	}}
	produced, err := RunTools(context.Background(), fake, []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, []Tool{searchTool(&query), failing}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"unknown tool", "invalid arguments", "database is down"} {
		if msg := produced[2*i+1]; msg.Role != llm.RoleTool || !strings.Contains(msg.Content, want) {
			t.Errorf("tool result %d = %+v, want error mentioning %q", i, msg, want)
		}
	}
	if query != "" {
		t.Error("search ran with invalid arguments")
	}
}

// The last round offers no tools and says why, so a model that searched a lot
// still answers in text instead of the reply breaking off.
func TestRunToolsLastRoundMustAnswer(t *testing.T) {
	var query string
	fake := &llm.Fake{}
	for range maxToolRounds - 1 {
		fake.Replies = append(fake.Replies, call("c", "search", `{"q":"again"}`))
	}
	fake.Replies = append(fake.Replies, answer("找到这些"))
	produced, err := RunTools(context.Background(), fake, []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, []Tool{searchTool(&query)}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := produced[len(produced)-1].Content; got != "找到这些" {
		t.Fatalf("last message %q", got)
	}
	for i, req := range fake.Requests {
		last := i == maxToolRounds-1
		if (len(req.Tools) == 0) != last {
			t.Errorf("request %d offered %d tools", i, len(req.Tools))
		}
		note := req.Messages[len(req.Messages)-1]
		if (note.Role == llm.RoleSystem && note.Content == lastRoundNote) != last {
			t.Errorf("request %d: last message %+v", i, note)
		}
	}
	// The note is only sent, never returned for storing.
	for _, m := range produced {
		if m.Content == lastRoundNote {
			t.Error("the note was returned for storing")
		}
	}
}

// A model that asks for tools even on the last round is stopped. Its
// unanswered tool call is not returned, since a stored history with one would
// be rejected by the model API next time.
func TestRunToolsStopsAfterMaxRounds(t *testing.T) {
	var query string
	fake := &llm.Fake{}
	for range maxToolRounds {
		fake.Replies = append(fake.Replies, call("c", "search", `{"q":"again"}`))
	}
	produced, err := RunTools(context.Background(), fake, []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, []Tool{searchTool(&query)}, "", nil)
	if !errors.Is(err, ErrTooManyToolRounds) {
		t.Fatalf("err = %v, want ErrTooManyToolRounds", err)
	}
	if len(fake.Requests) != maxToolRounds || len(produced) != 2*(maxToolRounds-1) {
		t.Fatalf("%d requests, %d messages; want %d and %d", len(fake.Requests), len(produced), maxToolRounds, 2*(maxToolRounds-1))
	}
	if last := produced[len(produced)-1]; last.Role != llm.RoleTool {
		t.Fatalf("history ends with %+v, want the last tool result", last)
	}
}

// The reminder ends every request, after the newest tool result, and is never
// returned for storing.
func TestRunToolsReminderEndsEveryRequest(t *testing.T) {
	var query string
	fake := &llm.Fake{Replies: []llm.Message{call("c1", "search", `{"q":"go"}`), answer("done")}}
	produced, err := RunTools(context.Background(), fake, []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, []Tool{searchTool(&query)}, "use English", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, req := range fake.Requests {
		if end := req.Messages[len(req.Messages)-1]; end.Role != llm.RoleSystem || end.Content != "use English" {
			t.Errorf("request %d ends with %+v", i, end)
		}
	}
	// The second request: user, tool call, tool result, then the reminder.
	if second := fake.Requests[1].Messages; len(second) != 4 || second[2].Role != llm.RoleTool {
		t.Errorf("second request: %+v", second)
	}
	for _, m := range produced {
		if m.Content == "use English" {
			t.Error("the reminder was returned for storing")
		}
	}
}
