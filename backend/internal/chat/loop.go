package chat

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ushi02/go_feed/backend/internal/llm"
)

// Tool is something the model may call: the definition sent to the model and
// the Go function that runs it. Run returns text (usually JSON) for the model.
type Tool struct {
	Def llm.ToolDef
	Run func(ctx context.Context, args json.RawMessage) (string, error)
}

// maxToolRounds caps how many times the model may ask for tools in one turn,
// so a confused model cannot loop (and spend tokens) forever.
const maxToolRounds = 5

var ErrTooManyToolRounds = errors.New("model kept calling tools")

// RunTools sends the conversation to the model and runs the tools it asks for,
// feeding each result back, until the model answers in plain text. It returns
// every message produced on the way (tool calls, tool results, the answer),
// also on error, so the caller can store them exactly as they were.
func RunTools(ctx context.Context, client llm.Client, history []llm.Message, tools []Tool) ([]llm.Message, error) {
	byName := make(map[string]Tool, len(tools))
	defs := make([]llm.ToolDef, len(tools))
	for i, t := range tools {
		byName[t.Def.Name] = t
		defs[i] = t.Def
	}
	messages := append([]llm.Message(nil), history...)
	var produced []llm.Message
	add := func(m llm.Message) {
		produced = append(produced, m)
		messages = append(messages, m)
	}
	for range maxToolRounds {
		reply, err := client.Chat(ctx, llm.Request{Messages: messages, Tools: defs})
		if err != nil {
			return produced, err
		}
		add(reply)
		if len(reply.ToolCalls) == 0 {
			return produced, nil
		}
		for _, c := range reply.ToolCalls {
			add(llm.Message{Role: llm.RoleTool, ToolCallID: c.ID, Content: runTool(ctx, byName, c)})
		}
	}
	return produced, ErrTooManyToolRounds
}

// runTool never fails the turn: an unknown tool, bad arguments or a tool error
// become the tool's result, so the model can see what went wrong and adjust.
func runTool(ctx context.Context, byName map[string]Tool, c llm.ToolCall) string {
	t, ok := byName[c.Function.Name]
	if !ok {
		return "error: unknown tool " + c.Function.Name
	}
	args := json.RawMessage(c.Function.Arguments)
	if c.Function.Arguments == "" {
		args = json.RawMessage("{}")
	}
	if !json.Valid(args) {
		return "error: invalid arguments: not valid JSON"
	}
	out, err := t.Run(ctx, args)
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}
