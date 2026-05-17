package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	anthropicAPIURL    = "https://api.anthropic.com/v1/messages"
	anthropicVersion   = "2023-06-01"
	anthropicMaxTokens = 4096
	anthropicTimeout   = 60 * time.Second
)

// AnthropicClient implements LLM by calling the Anthropic Messages API
// directly over HTTP. No SDK — the dependency surface stays small.
type AnthropicClient struct {
	apiKey string
	model  string
	http   *http.Client
}

// NewAnthropicClient validates the configuration up front so a missing key
// or wrong-provider model never reaches a chat request.
func NewAnthropicClient(apiKey, model string) (*AnthropicClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY is required for claude-* models")
	}
	if !strings.HasPrefix(model, "claude-") {
		return nil, fmt.Errorf("anthropic adapter requires a claude-* model, got %q", model)
	}
	return &AnthropicClient{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: anthropicTimeout},
	}, nil
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
}

type anthropicErrorBody struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *AnthropicClient) Chat(ctx context.Context, system string, messages []Message, tools []Tool) (ChatResponse, error) {
	body := anthropicRequest{
		Model:     c.model,
		MaxTokens: anthropicMaxTokens,
		System:    system,
		Messages:  make([]anthropicMessage, 0, len(messages)),
	}
	for _, m := range messages {
		body.Messages = append(body.Messages, anthropicMessage{Role: string(m.Role), Content: m.Content})
	}
	for _, t := range tools {
		body.Tools = append(body.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("encode anthropic request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(buf))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("build anthropic request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("read anthropic response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr anthropicErrorBody
		if jerr := json.Unmarshal(respBody, &apiErr); jerr == nil && apiErr.Error.Message != "" {
			return ChatResponse{}, fmt.Errorf("anthropic %d %s: %s", resp.StatusCode, apiErr.Error.Type, apiErr.Error.Message)
		}
		return ChatResponse{}, fmt.Errorf("anthropic %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return ChatResponse{}, fmt.Errorf("decode anthropic response: %w", err)
	}

	var text strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(block.Text)
		}
	}
	return ChatResponse{Text: text.String()}, nil
}
