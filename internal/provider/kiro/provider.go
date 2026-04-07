package kiro

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hoazgazh/aigate/internal/config"
	"github.com/hoazgazh/aigate/internal/provider"
)

// Provider implements provider.Provider for Kiro API.
type Provider struct {
	cfg  *config.Config
	auth *AuthManager
	http *http.Client
}

// New creates a new Kiro provider with initialized auth.
func New(cfg *config.Config) (*Provider, error) {
	auth, err := NewAuthManager(
		cfg.RefreshToken,
		cfg.ProfileARN,
		cfg.KiroRegion,
		cfg.CredsFile,
		cfg.CLIDBFile,
	)
	if err != nil {
		return nil, fmt.Errorf("init kiro auth: %w", err)
	}

	return &Provider{
		cfg:  cfg,
		auth: auth,
		http: &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (p *Provider) Name() string { return "kiro" }

func (p *Provider) Auth() *AuthManager { return p.auth }

func (p *Provider) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	token, err := p.auth.GetToken()
	if err != nil {
		return nil, err
	}

	url := p.auth.QHost + "/listAvailableModels"
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader("{}"))
	for k, v := range kiroHeaders(token, p.auth.fingerprint) {
		req.Header.Set(k, v)
	}
	if p.auth.ProfileARN() != "" {
		body, _ := json.Marshal(map[string]string{"profileArn": p.auth.ProfileARN()})
		req.Body = io.NopCloser(bytes.NewReader(body))
	}

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list models: status %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Models []struct {
			ModelID string `json:"modelId"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse models: %w", err)
	}

	var models []provider.ModelInfo
	for _, m := range result.Models {
		models = append(models, provider.ModelInfo{
			ID:      m.ModelID,
			Object:  "model",
			OwnedBy: "kiro",
		})
	}
	return models, nil
}

func (p *Provider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	// Use streaming internally and collect
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return nil, err
	}

	var content strings.Builder
	var finishReason string
	for chunk := range ch {
		if chunk.Error != nil {
			return nil, chunk.Error
		}
		content.WriteString(chunk.Content)
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
	}

	if finishReason == "" {
		finishReason = "stop"
	}

	return &provider.CompletionResponse{
		Content:      content.String(),
		FinishReason: finishReason,
		Model:        req.Model,
	}, nil
}

func (p *Provider) Stream(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	// Convert messages
	var msgs []Message
	for _, m := range req.Messages {
		msgs = append(msgs, Message{Role: m.Role, Content: m.Content})
	}

	profileARN := ""
	if p.auth.creds.AuthType == AuthKiroDesktop {
		profileARN = p.auth.ProfileARN()
	}

	payload, err := buildKiroPayload(req.Model, msgs, profileARN)
	if err != nil {
		return nil, fmt.Errorf("build payload: %w", err)
	}

	// Make request with retry
	resp, err := p.doRequestWithRetry(ctx, payload)
	if err != nil {
		return nil, err
	}

	ch := make(chan provider.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		parser := &StreamParser{}
		scanner := bufio.NewReaderSize(resp.Body, 32*1024)

		buf := make([]byte, 32*1024)
		for {
			n, err := scanner.Read(buf)
			if n > 0 {
				events := parser.Feed(buf[:n])
				for _, ev := range events {
					if ev.Type == "content" {
						ch <- provider.StreamChunk{Content: ev.Content}
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					ch <- provider.StreamChunk{Error: err}
				}
				break
			}
		}
		ch <- provider.StreamChunk{FinishReason: "stop"}
	}()

	return ch, nil
}

func (p *Provider) doRequestWithRetry(ctx context.Context, payload map[string]any) (*http.Response, error) {
	url := p.auth.APIHost + "/generateAssistantResponse"
	maxRetries := p.cfg.FirstTokenMaxRetries

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		headers, err := p.auth.Headers()
		if err != nil {
			return nil, fmt.Errorf("get headers: %w", err)
		}

		body, _ := json.Marshal(payload)
		req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Connection", "close")

		resp, err := p.http.Do(req)
		if err != nil {
			lastErr = err
			log.Printf("[kiro] request error (attempt %d/%d): %v", attempt+1, maxRetries, err)
			time.Sleep(time.Duration(1<<attempt) * time.Second)
			continue
		}

		if resp.StatusCode == 200 {
			return resp, nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		switch {
		case resp.StatusCode == 403:
			log.Printf("[kiro] 403 received, refreshing token (attempt %d/%d)", attempt+1, maxRetries)
			p.auth.ForceRefresh()
		case resp.StatusCode == 429:
			delay := time.Duration(1<<attempt) * time.Second
			log.Printf("[kiro] 429 rate limited, waiting %v (attempt %d/%d)", delay, attempt+1, maxRetries)
			time.Sleep(delay)
		case resp.StatusCode >= 500:
			delay := time.Duration(1<<attempt) * time.Second
			log.Printf("[kiro] %d server error, waiting %v (attempt %d/%d)", resp.StatusCode, delay, attempt+1, maxRetries)
			time.Sleep(delay)
		default:
			return nil, fmt.Errorf("kiro API error: status %d, body: %s", resp.StatusCode, respBody)
		}
		lastErr = fmt.Errorf("status %d: %s", resp.StatusCode, respBody)
	}

	return nil, fmt.Errorf("all %d attempts failed: %w", maxRetries, lastErr)
}

// completionID generates a unique completion ID.
func completionID() string {
	return "chatcmpl-" + uuid.New().String()[:8]
}
