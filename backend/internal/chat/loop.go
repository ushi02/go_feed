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

// errorPrefix starts a tool result that reports a failure instead of data.
const errorPrefix = "error: "

// maxReplyTokens keeps answers short; chatting with Gugu should not cost much.
const maxReplyTokens = 800

// TextFilter sits between the model's streamed text and the user. Write gets
// each piece as it arrives; Close ends one model message and returns the text
// to keep, which replaces the message's content. So what is stored is exactly
// what the user saw.
type TextFilter interface {
	Write(piece string)
	Close() string
}

// RunTools sends the conversation to the model and runs the tools it asks for,
// feeding each result back, until the model answers in plain text. It returns
// every message produced on the way (tool calls, tool results, the answer),
// also on error, so the caller can store them exactly as they were. If the
// model's stream breaks, the text received so far is the last message.
// filter may be nil.
func RunTools(ctx context.Context, client llm.Client, history []llm.Message, tools []Tool, filter TextFilter) ([]llm.Message, error) {
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
	var onText func(string)
	if filter != nil {
		onText = filter.Write
	}
	for range maxToolRounds {
		reply, err := client.Chat(ctx, llm.Request{Messages: messages, Tools: defs, MaxTokens: maxReplyTokens}, onText)
		if filter != nil {
			reply.Content = filter.Close()
		}
		if err != nil {
			if reply.Content != "" {
				add(llm.Message{Role: llm.RoleAssistant, Content: reply.Content})
			}
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
		return errorPrefix + "unknown tool " + c.Function.Name
	}
	args := json.RawMessage(c.Function.Arguments)
	if c.Function.Arguments == "" {
		args = json.RawMessage("{}")
	}
	if !json.Valid(args) {
		return errorPrefix + "invalid arguments: not valid JSON"
	}
	out, err := t.Run(ctx, args)
	if err != nil {
		return errorPrefix + err.Error()
	}
	return out
}
