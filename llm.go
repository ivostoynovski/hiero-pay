package main

import "context"

// Role enumerates valid sender roles in a chat transcript.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one entry in a chat transcript. The Tool role and tool-call
// plumbing are deferred to Slice 3 — the interface accepts them now only
// because the Anthropic Messages API requires them in the same call.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Tool describes a callable function exposed to the LLM. The schema is the
// JSON-Schema-compatible shape that the model fills in when it decides to
// invoke the tool.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolCall is a single invocation produced by the model in a turn.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ChatResponse is the assistant's reply for a single Chat call.
type ChatResponse struct {
	Text      string     `json:"text"`
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
}

// LLM is the provider-agnostic chat surface. Slice 2 ships a single
// implementation (Anthropic); Slice 5 adds OpenAI behind the same interface.
type LLM interface {
	Chat(ctx context.Context, system string, messages []Message, tools []Tool) (ChatResponse, error)
}
