// Package llm talks to a chat model. The rest of the code depends only on
// Client; DeepSeek is the real implementation and Fake replays scripted
// replies in tests.
package llm

import (
	"context"
	"encoding/json"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one chat message in the OpenAI-compatible format, so it can be
// stored and sent back to the model exactly as it was.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant asking to run tools
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool result answering that call
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // always "function"
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name string `json:"name"`
	// Arguments is a JSON object encoded as a string. The model writes it, so
	// it may be invalid JSON and must be checked before use.
	Arguments string `json:"arguments"`
}

// ToolDef describes a tool the model may call. Parameters is a JSON Schema.
type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type Request struct {
	Messages []Message
	Tools    []ToolDef
}

type Client interface {
	// Chat sends the conversation and returns the model's next message: either
	// a text answer or a request to call tools.
	Chat(ctx context.Context, req Request) (Message, error)
}
