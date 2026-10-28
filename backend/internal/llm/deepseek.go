package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	Model    string     `json:"model"`
	Messages []Message  `json:"messages"`
	Tools    []wireTool `json:"tools,omitempty"`
	// Thinking mode is on by default. Recommending blogs needs no long
	// reasoning, and turning it off makes replies faster and cheaper.
	Thinking map[string]string `json:"thinking"`
	Stream   bool              `json:"stream"`
}

type wireResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
}

func (d *DeepSeek) Chat(ctx context.Context, req Request) (Message, error) {
	body := wireRequest{Model: d.model, Messages: req.Messages, Thinking: map[string]string{"type": "disabled"}}
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
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Message{}, fmt.Errorf("deepseek: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("deepseek: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var out wireResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Message{}, fmt.Errorf("deepseek: decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		return Message{}, errors.New("deepseek: response has no choices")
	}
	return out.Choices[0].Message, nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
