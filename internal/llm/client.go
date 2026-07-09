package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	Provider  string
	BaseURL   string
	APIKey    string
	Model     string
	HTTP      *http.Client
	UserAgent string
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
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", err
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
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8_000_000))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("LLM HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed chatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("LLM response has no choices")
	}
	return parsed.Choices[0].Message.Content, nil
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}
