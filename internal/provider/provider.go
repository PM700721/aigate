package provider

import "context"

// Message represents a chat message in unified format.
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// StreamChunk represents a single chunk from streaming response.
type StreamChunk struct {
	Content          string
	ReasoningContent string
	FinishReason     string
	Error            error
}

// CompletionRequest is the unified request format sent to providers.
type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	Tools       []any     `json:"tools,omitempty"`
}

// CompletionResponse is the unified non-streaming response.
type CompletionResponse struct {
	Content      string
	FinishReason string
	Model        string
	InputTokens  int
	OutputTokens int
}

// ModelInfo describes an available model.
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

// Provider is the interface all AI backends must implement.
type Provider interface {
	// Name returns the provider identifier.
	Name() string

	// ListModels returns available models.
	ListModels(ctx context.Context) ([]ModelInfo, error)

	// Complete sends a non-streaming request.
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

	// Stream sends a streaming request and returns a channel of chunks.
	Stream(ctx context.Context, req *CompletionRequest) (<-chan StreamChunk, error)
}
