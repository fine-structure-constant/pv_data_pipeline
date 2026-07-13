package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	Provider        string
	BaseURL         string
	APIKey          string
	Model           string
	HTTP            *http.Client
	UserAgent       string
	EnableWebSearch bool
	WebSearchURL    string
}

func (c Client) Enabled() bool {
	return strings.TrimSpace(c.Provider) != "" && strings.TrimSpace(c.APIKey) != ""
}

func (c Client) Classify(ctx context.Context, prompt, input string) (string, error) {
	if !c.Enabled() {
		return "", errors.New("LLM disabled: LLM_PROVIDER or LLM_API_KEY is empty")
	}
	body := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: prompt},
			{Role: "user", Content: input},
		},
		Temperature: 0,
	}
	if c.EnableWebSearch {
		body.Tools = []chatTool{{Type: "function", Function: toolFunction{Name: "web_search", Description: "Search Crossref for scholarly literature when metadata is insufficient.", Parameters: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []string{"query"}}}}}
	}
	for round := 0; round < 3; round++ {
		content, calls, err := c.doChat(ctx, body)
		if err != nil {
			return "", err
		}
		if len(calls) == 0 {
			return content, nil
		}
		body.Messages = append(body.Messages, chatMessage{Role: "assistant", Content: content, ToolCalls: calls})
		for _, call := range calls {
			result := c.runTool(ctx, call)
			body.Messages = append(body.Messages, chatMessage{Role: "tool", Content: result, ToolCallID: call.ID})
		}
	}
	return "", errors.New("LLM exceeded tool-call rounds")
}

func (c Client) doChat(ctx context.Context, body chatRequest) (string, []toolCall, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8_000_000))
	if resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", nil, err
	}
	if len(parsed.Choices) == 0 {
		return "", nil, errors.New("LLM response has no choices")
	}
	return parsed.Choices[0].Message.Content, parsed.Choices[0].Message.ToolCalls, nil
}

func (c Client) runTool(ctx context.Context, call toolCall) string {
	if call.Function.Name != "web_search" {
		return `{"error":"unknown tool"}`
	}
	var args struct {
		Query string `json:"query"`
	}
	if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || strings.TrimSpace(args.Query) == "" {
		return `{"error":"query is required"}`
	}
	base := c.WebSearchURL
	if base == "" {
		base = "https://api.crossref.org/works"
	}
	u := strings.TrimRight(base, "?") + "?rows=5&select=DOI,title,abstract,URL&query.bibliographic=" + url.QueryEscape(args.Query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	req.Header.Set("User-Agent", c.UserAgent)
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1_000_000))
	if resp.StatusCode >= 300 {
		return fmt.Sprintf(`{"error":"Crossref HTTP %d"}`, resp.StatusCode)
	}
	var payload struct {
		Message struct {
			Items []struct {
				DOI      string   `json:"DOI"`
				Title    []string `json:"title"`
				Abstract string   `json:"abstract"`
				URL      string   `json:"URL"`
			} `json:"items"`
		} `json:"message"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return fmt.Sprintf(`{"error":%q}`, "invalid Crossref response: "+err.Error())
	}
	type result struct {
		DOI      string `json:"doi"`
		Title    string `json:"title"`
		Abstract string `json:"abstract,omitempty"`
		URL      string `json:"url,omitempty"`
	}
	results := make([]result, 0, len(payload.Message.Items))
	for _, item := range payload.Message.Items {
		title := ""
		if len(item.Title) > 0 {
			title = item.Title[0]
		}
		abstract := strings.TrimSpace(item.Abstract)
		if len(abstract) > 1200 {
			abstract = abstract[:1200]
		}
		results = append(results, result{DOI: item.DOI, Title: title, Abstract: abstract, URL: item.URL})
	}
	out, _ := json.Marshal(map[string]any{"query": args.Query, "results": results})
	return string(out)
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Tools       []chatTool    `json:"tools,omitempty"`
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}
type toolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}
type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}
