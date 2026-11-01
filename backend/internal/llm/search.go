package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// WebResult is one web page found by Search.
type WebResult struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	SiteName string `json:"site_name"`
	Date     string `json:"date"`
	// Summary is DeepSeek's longer description of the page, written for models.
	Summary string `json:"summary"`
}

// maxSearchBytes caps the response read into memory; count is small, so a
// real answer is far below this.
const maxSearchBytes = 1 << 20

// Search asks DeepSeek's web search (POST /v1/tools/search, billed per
// successful request) for count results. It goes to the same fixed host as
// Chat, with the same key, so it uses the same client, not fetcher.
func (d *DeepSeek) Search(ctx context.Context, query string, count int) ([]WebResult, error) {
	data, err := json.Marshal(map[string]any{"query": query, "count": count})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/v1/tools/search", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepseek search: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSearchBytes))
	if err != nil {
		return nil, fmt.Errorf("deepseek search: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 402 means the account balance ran out.
		return nil, fmt.Errorf("deepseek search: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var body struct {
		Results []WebResult `json:"results"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("deepseek search: decode: %w", err)
	}
	return body.Results, nil
}
