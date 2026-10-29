package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	deepSeekURL   = "https://api.deepseek.com"
	deepSeekModel = "deepseek-flash"
)

// DeepSeek calls the DeepSeek chat API (OpenAI-compatible). It talks to one
// fixed, trusted host, so it uses its own HTTP client instead of fetcher,
// whose SSRF guard is for user-supplied URLs.
type DeepSeek struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

// NewDeepSeekFromEnv reads DEEPSEEK_API_KEY (required) and DEEPSEEK_MODEL
// (optional, default deepseek-flash). Keys never live in code.
func NewDeepSeekFromEnv() (*DeepSeek, error) {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		return nil, errors.New("DEEPSEEK_API_KEY is not set")
	}
	model := os.Getenv("DEEPSEEK_MODEL")
	if model == "" {
		model = deepSeekModel
	}
	return &DeepSeek{apiKey: key, model: model, baseURL: deepSeekURL, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireRequest struct {
	Model     string     `json:"model"`
	Messages  []Message  `json:"messages"`
	Tools     []wireTool `json:"tools,omitempty"`
	MaxTokens int        `json:"max_tokens,omitempty"`
	// Thinking mode is on by default. Recommending blogs needs no long
	// reasoning, and turning it off makes replies faster and cheaper.
	Thinking map[string]string `json:"thinking"`
	Stream   bool              `json:"stream"`
}

// wireChunk is one server-sent event of a streamed reply. Text arrives in
// pieces; a tool call arrives as its ID and name first, then its arguments
// in pieces, matched up by index.
type wireChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// maxReplyBytes bounds how much one streamed reply may hold in memory.
const maxReplyBytes = 1 << 20

func (d *DeepSeek) Chat(ctx context.Context, req Request, onText func(string)) (Message, error) {
	body := wireRequest{Model: d.model, Messages: req.Messages, MaxTokens: req.MaxTokens, Thinking: map[string]string{"type": "disabled"}, Stream: true}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, wireTool{Type: "function", Function: wireFunction{Name: t.Name, Description: t.Description, Parameters: t.Parameters}})
	}
	data, err := json.Marshal(body)
	if err != nil {
		return Message{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return Message{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+d.apiKey)
	resp, err := d.http.Do(httpReq)
	if err != nil {
		return Message{}, fmt.Errorf("deepseek: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return Message{}, fmt.Errorf("deepseek: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	return readStream(resp.Body, onText)
}

// readStream reads server-sent events ("data: {...}" lines, ending with
// "data: [DONE]") and puts the pieces back together into one message.
func readStream(r io.Reader, onText func(string)) (Message, error) {
	var text strings.Builder
	var calls []ToolCall
	partial := func(err error) (Message, error) {
		return Message{Role: RoleAssistant, Content: text.String()}, fmt.Errorf("deepseek: stream: %w", err)
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxReplyBytes)
	for sc.Scan() {
		payload, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue // blank separators and ": keep-alive" comments
		}
		payload = strings.TrimSpace(payload)
		if payload == "[DONE]" {
			return Message{Role: RoleAssistant, Content: text.String(), ToolCalls: calls}, nil
		}
		var chunk wireChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			return partial(fmt.Errorf("decode chunk: %w", err))
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			if text.Len()+len(delta.Content) > maxReplyBytes {
				return partial(errors.New("reply too long"))
			}
			text.WriteString(delta.Content)
			if onText != nil {
				onText(delta.Content)
			}
		}
		for _, tc := range delta.ToolCalls {
			if tc.Index < 0 || tc.Index > 16 {
				return partial(fmt.Errorf("tool call index %d out of range", tc.Index))
			}
			for len(calls) <= tc.Index {
				calls = append(calls, ToolCall{Type: "function"})
			}
			c := &calls[tc.Index]
			if tc.ID != "" {
				c.ID = tc.ID
			}
			c.Function.Name += tc.Function.Name
			c.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := sc.Err(); err != nil {
		return partial(err)
	}
	return partial(io.ErrUnexpectedEOF) // the stream ended without [DONE]
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
