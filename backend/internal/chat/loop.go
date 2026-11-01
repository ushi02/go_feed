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

// maxToolRounds caps how many times the model is called in one turn, so a
// confused model cannot loop (and spend tokens) forever. The last call offers
// no tools, so the model has to answer with what it found.
const maxToolRounds = 5

// lastRoundNote is added (not stored) to the last call, so the model knows
// why the tools are gone.
const lastRoundNote = "工具调用次数已经用完。根据已经拿到的结果，直接用文字回答用户。"

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
// feeding each result back, until the model answers in plain text, at the
// latest on the last round, which offers no tools. It returns
// every message produced on the way (tool calls, tool results, the answer),
// also on error, so the caller can store them exactly as they were. If the
// model's stream breaks, the text received so far is the last message.
// reminder, if not empty, ends every request (it is never returned, so never
// stored): an instruction placed after the latest tool results is followed
// more reliably than one at the start of a long conversation. filter may be nil.
func RunTools(ctx context.Context, client llm.Client, history []llm.Message, tools []Tool, reminder string, filter TextFilter) ([]llm.Message, error) {
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
	for round := range maxToolRounds {
		req := llm.Request{Messages: messages, Tools: defs, MaxTokens: maxReplyTokens}
		var notes []llm.Message
		if reminder != "" {
			notes = append(notes, llm.Message{Role: llm.RoleSystem, Content: reminder})
		}
		last := round == maxToolRounds-1
		if last {
			notes = append(notes, llm.Message{Role: llm.RoleSystem, Content: lastRoundNote})
			req.Tools = nil
		}
		// The full slice expression makes append copy, so the notes never
		// end up in messages.
		req.Messages = append(messages[:len(messages):len(messages)], notes...)
		reply, err := client.Chat(ctx, req, onText)
		if filter != nil {
			reply.Content = filter.Close()
		}
		if err != nil {
			if reply.Content != "" {
				add(llm.Message{Role: llm.RoleAssistant, Content: reply.Content})
			}
			return produced, err
		}
		if len(reply.ToolCalls) == 0 {
			add(reply)
			return produced, nil
		}
		if last {
			// Tool calls nobody answers must not be stored: the model API
			// rejects a history with an unanswered tool call. Text the user
			// already saw is kept.
			if reply.Content != "" {
				add(llm.Message{Role: llm.RoleAssistant, Content: reply.Content})
			}
			break
		}
		add(reply)
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
