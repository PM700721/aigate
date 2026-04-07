package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hoazgazh/aigate/internal/config"
	"github.com/hoazgazh/aigate/internal/middleware"
	"github.com/hoazgazh/aigate/internal/provider"
	"github.com/hoazgazh/aigate/internal/provider/copilot"
	"github.com/hoazgazh/aigate/internal/provider/kiro"
)

type server struct {
	cfg       *config.Config
	providers map[string]provider.Provider // model prefix → provider
	fallback  provider.Provider            // default provider
}

// NewRouterWithProvider creates the HTTP router with initialized providers.
func NewRouterWithProvider(cfg *config.Config) (http.Handler, error) {
	s := &server{
		cfg:       cfg,
		providers: make(map[string]provider.Provider),
	}

	// Init Kiro provider (primary)
	kiroProvider, err := kiro.New(cfg)
	if err != nil {
		return nil, err
	}
	s.fallback = kiroProvider

	// Init Copilot provider if token exists or login requested
	copilotProvider, err := copilot.New()
	if err == nil && !copilotProvider.NeedsLogin() {
		s.providers["copilot"] = copilotProvider
		log.Printf("[api] copilot provider enabled")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleHealth)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /v1/models", s.handleModels)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("POST /v1/messages", s.handleMessages)

	var handler http.Handler = mux
	handler = middleware.Auth(cfg.APIKey)(handler)
	return handler, nil
}

// resolveProvider picks the right provider based on model name.
// Models prefixed with "copilot/" go to Copilot, everything else to Kiro.
func (s *server) resolveProvider(model string) (provider.Provider, string) {
	if strings.HasPrefix(model, "copilot/") {
		if p, ok := s.providers["copilot"]; ok {
			return p, strings.TrimPrefix(model, "copilot/")
		}
	}
	return s.fallback, model
}

// CopilotProvider returns the copilot provider for login flow (may be nil).
func (s *server) CopilotProvider() *copilot.Provider {
	if p, ok := s.providers["copilot"]; ok {
		return p.(*copilot.Provider)
	}
	return nil
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": config.Version,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *server) handleModels(w http.ResponseWriter, r *http.Request) {
	var allModels []provider.ModelInfo

	// Kiro models
	if models, err := s.fallback.ListModels(r.Context()); err == nil {
		allModels = append(allModels, models...)
	} else {
		allModels = append(allModels, fallbackModels()...)
	}

	// Copilot models (prefixed)
	if cp, ok := s.providers["copilot"]; ok {
		if models, err := cp.ListModels(r.Context()); err == nil {
			for _, m := range models {
				m.ID = "copilot/" + m.ID
				allModels = append(allModels, m)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   allModels,
	})
}

func (s *server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model       string    `json:"model"`
		Messages    []msgJSON `json:"messages"`
		Stream      bool      `json:"stream"`
		MaxTokens   int       `json:"max_tokens,omitempty"`
		Temperature *float64  `json:"temperature,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp("invalid request: "+err.Error()))
		return
	}

	if len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, errorResp("messages is required"))
		return
	}

	// Convert to provider format
	var msgs []provider.Message
	for _, m := range req.Messages {
		msgs = append(msgs, provider.Message{Role: m.Role, Content: m.Content})
	}

	// Resolve provider based on model prefix
	prov, resolvedModel := s.resolveProvider(req.Model)

	provReq := &provider.CompletionRequest{
		Model:       resolvedModel,
		Messages:    msgs,
		Stream:      req.Stream,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}

	log.Printf("[api] POST /v1/chat/completions model=%s stream=%v messages=%d", req.Model, req.Stream, len(req.Messages))

	if req.Stream {
		s.handleStream(w, r, provReq, prov)
	} else {
		s.handleNonStream(w, r, provReq, prov)
	}
}

func (s *server) handleStream(w http.ResponseWriter, r *http.Request, req *provider.CompletionRequest, prov provider.Provider) {
	ch, err := prov.Stream(r.Context(), req)
	if err != nil {
		log.Printf("[api] stream error: %v", err)
		writeJSON(w, http.StatusBadGateway, errorResp(err.Error()))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorResp("streaming not supported"))
		return
	}

	id := "chatcmpl-" + uuid.New().String()[:8]

	// Send role chunk first
	roleChunk := map[string]any{
		"id":     id,
		"object": "chat.completion.chunk",
		"model":  req.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"role": "assistant"},
			"finish_reason": nil,
		}},
	}
	b, _ := json.Marshal(roleChunk)
	fmt.Fprintf(w, "data: %s\n\n", b)
	flusher.Flush()

	for chunk := range ch {
		if chunk.Error != nil {
			log.Printf("[api] stream chunk error: %v", chunk.Error)
			break
		}
		if chunk.Content != "" {
			data := kiro.FormatSSEChunk(id, req.Model, chunk.Content, nil)
			io.WriteString(w, data)
			flusher.Flush()
		}
		if chunk.FinishReason != "" {
			reason := chunk.FinishReason
			data := kiro.FormatSSEChunk(id, req.Model, "", &reason)
			io.WriteString(w, data)
			flusher.Flush()
		}
	}

	io.WriteString(w, kiro.FormatSSEDone())
	flusher.Flush()
}

func (s *server) handleNonStream(w http.ResponseWriter, r *http.Request, req *provider.CompletionRequest, prov provider.Provider) {
	resp, err := prov.Complete(r.Context(), req)
	if err != nil {
		log.Printf("[api] complete error: %v", err)
		writeJSON(w, http.StatusBadGateway, errorResp(err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     "chatcmpl-" + uuid.New().String()[:8],
		"object": "chat.completion",
		"model":  resp.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": resp.Content},
			"finish_reason": resp.FinishReason,
		}},
		"usage": map[string]any{
			"prompt_tokens":     resp.InputTokens,
			"completion_tokens": resp.OutputTokens,
			"total_tokens":      resp.InputTokens + resp.OutputTokens,
		},
	})
}

func (s *server) handleMessages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model     string    `json:"model"`
		Messages  []msgJSON `json:"messages"`
		System    string    `json:"system"`
		MaxTokens int       `json:"max_tokens"`
		Stream    bool      `json:"stream"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResp("invalid request: "+err.Error()))
		return
	}

	// Prepend system as first message
	var msgs []provider.Message
	if req.System != "" {
		msgs = append(msgs, provider.Message{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, provider.Message{Role: m.Role, Content: m.Content})
	}

	prov, resolvedModel := s.resolveProvider(req.Model)

	provReq := &provider.CompletionRequest{
		Model:     resolvedModel,
		Messages:  msgs,
		Stream:    req.Stream,
		MaxTokens: req.MaxTokens,
	}

	log.Printf("[api] POST /v1/messages model=%s stream=%v", req.Model, req.Stream)

	if req.Stream {
		s.handleAnthropicStream(w, r, provReq, prov)
	} else {
		s.handleAnthropicNonStream(w, r, provReq, prov)
	}
}

func (s *server) handleAnthropicStream(w http.ResponseWriter, r *http.Request, req *provider.CompletionRequest, prov provider.Provider) {
	ch, err := prov.Stream(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResp(err.Error()))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	// message_start
	msgID := "msg_" + uuid.New().String()[:8]
	writeSSE(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":    msgID,
			"type":  "message",
			"role":  "assistant",
			"model": req.Model,
		},
	})
	// content_block_start
	writeSSE(w, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         0,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	if flusher != nil {
		flusher.Flush()
	}

	for chunk := range ch {
		if chunk.Error != nil {
			break
		}
		if chunk.Content != "" {
			writeSSE(w, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": chunk.Content},
			})
			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	writeSSE(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	writeSSE(w, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn"},
	})
	writeSSE(w, "message_stop", map[string]any{"type": "message_stop"})
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *server) handleAnthropicNonStream(w http.ResponseWriter, r *http.Request, req *provider.CompletionRequest, prov provider.Provider) {
	resp, err := prov.Complete(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorResp(err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":           "msg_" + uuid.New().String()[:8],
		"type":         "message",
		"role":         "assistant",
		"model":        resp.Model,
		"content":      []any{map[string]any{"type": "text", "text": resp.Content}},
		"stop_reason":  resp.FinishReason,
	})
}

// --- helpers ---

type msgJSON struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeSSE(w http.ResponseWriter, event string, data any) {
	b, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}

func errorResp(msg string) map[string]any {
	return map[string]any{
		"error": map[string]string{
			"message": msg,
			"type":    "error",
		},
	}
}

func fallbackModels() []provider.ModelInfo {
	ids := []string{"auto", "claude-sonnet-4.5", "claude-haiku-4.5", "claude-sonnet-4", "claude-opus-4.5"}
	var models []provider.ModelInfo
	for _, id := range ids {
		models = append(models, provider.ModelInfo{ID: id, Object: "model", OwnedBy: "kiro"})
	}
	return models
}

// FormatSSEChunk is re-exported for use by router.
func init() {
	// Ensure kiro package's FormatSSEChunk is accessible
	_ = strings.Builder{}
}
