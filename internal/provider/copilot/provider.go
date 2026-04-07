package copilot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hoazgazh/aigate/internal/provider"
)

const (
	// GitHub OAuth device flow endpoints
	githubDeviceCodeURL = "https://github.com/login/device/code"
	githubAccessTokenURL = "https://github.com/login/oauth/access_token"

	// Copilot API endpoints
	copilotTokenURL = "https://api.github.com/copilot_internal/v2/token"
	copilotChatURL  = "https://api.githubcopilot.com/chat/completions"
	copilotModelsURL = "https://api.githubcopilot.com/models"

	// VS Code's OAuth client ID (public, used by all copilot clients)
	vsCodeClientID = "Iv1.b507a08c87ecfe98"
)

// Provider implements provider.Provider for GitHub Copilot.
type Provider struct {
	mu           sync.RWMutex
	githubToken  string
	copilotToken string
	copilotExpiry time.Time
	http         *http.Client
	tokenPath    string
}

// New creates a new Copilot provider.
func New() (*Provider, error) {
	home, _ := os.UserHomeDir()
	tokenPath := filepath.Join(home, ".local", "share", "aigate", "github_token")

	p := &Provider{
		http:      &http.Client{Timeout: 5 * time.Minute},
		tokenPath: tokenPath,
	}

	// Try to load saved GitHub token
	if data, err := os.ReadFile(tokenPath); err == nil {
		p.githubToken = strings.TrimSpace(string(data))
		log.Printf("[copilot] loaded GitHub token from %s", tokenPath)
	}

	return p, nil
}

func (p *Provider) Name() string { return "copilot" }

// NeedsLogin returns true if no GitHub token is available.
func (p *Provider) NeedsLogin() bool {
	return p.githubToken == ""
}

// Login performs GitHub OAuth device code flow.
// User must open browser and enter the code.
func (p *Provider) Login(ctx context.Context) error {
	// Step 1: Request device code
	body, _ := json.Marshal(map[string]string{
		"client_id": vsCodeClientID,
		"scope":     "read:user",
	})
	req, _ := http.NewRequestWithContext(ctx, "POST", githubDeviceCodeURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return fmt.Errorf("device code request: %w", err)
	}
	defer resp.Body.Close()

	var deviceResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		return fmt.Errorf("parse device code: %w", err)
	}

	fmt.Printf("\n🔑 GitHub Copilot Login\n")
	fmt.Printf("   1. Open: %s\n", deviceResp.VerificationURI)
	fmt.Printf("   2. Enter code: %s\n\n", deviceResp.UserCode)
	fmt.Printf("   Waiting for authorization...\n")

	// Step 2: Poll for access token
	interval := time.Duration(deviceResp.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(deviceResp.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		tokenBody, _ := json.Marshal(map[string]string{
			"client_id":   vsCodeClientID,
			"device_code": deviceResp.DeviceCode,
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		})
		tokenReq, _ := http.NewRequestWithContext(ctx, "POST", githubAccessTokenURL, bytes.NewReader(tokenBody))
		tokenReq.Header.Set("Content-Type", "application/json")
		tokenReq.Header.Set("Accept", "application/json")

		tokenResp, err := p.http.Do(tokenReq)
		if err != nil {
			continue
		}

		var result struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		json.NewDecoder(tokenResp.Body).Decode(&result)
		tokenResp.Body.Close()

		if result.AccessToken != "" {
			p.githubToken = result.AccessToken
			p.saveGitHubToken()
			fmt.Printf("   ✅ Logged in successfully!\n\n")
			return nil
		}

		if result.Error == "authorization_pending" {
			continue
		}
		if result.Error == "slow_down" {
			interval += 5 * time.Second
			continue
		}
		if result.Error != "" {
			return fmt.Errorf("auth error: %s", result.Error)
		}
	}

	return fmt.Errorf("login timed out")
}

// getCopilotToken returns a valid Copilot API token, refreshing if needed.
func (p *Provider) getCopilotToken() (string, error) {
	p.mu.RLock()
	if p.copilotToken != "" && time.Now().Before(p.copilotExpiry.Add(-5*time.Minute)) {
		tok := p.copilotToken
		p.mu.RUnlock()
		return tok, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check
	if p.copilotToken != "" && time.Now().Before(p.copilotExpiry.Add(-5*time.Minute)) {
		return p.copilotToken, nil
	}

	if p.githubToken == "" {
		return "", fmt.Errorf("not logged in — run aigate with --copilot-login")
	}

	// Exchange GitHub token for Copilot token
	req, _ := http.NewRequest("GET", copilotTokenURL, nil)
	req.Header.Set("Authorization", "token "+p.githubToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.27.0")
	req.Header.Set("Editor-Version", "vscode/1.100.0")

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("copilot token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("copilot token: status %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse copilot token: %w", err)
	}

	p.copilotToken = result.Token
	p.copilotExpiry = time.Unix(result.ExpiresAt, 0)
	log.Printf("[copilot] token refreshed, expires: %s", p.copilotExpiry.Format(time.RFC3339))

	return p.copilotToken, nil
}

func (p *Provider) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	token, err := p.getCopilotToken()
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", copilotModelsURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	req.Header.Set("Editor-Version", "vscode/1.100.0")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.27.0")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list models: %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	var models []provider.ModelInfo
	for _, m := range result.Data {
		models = append(models, provider.ModelInfo{
			ID: m.ID, Object: "model", OwnedBy: "copilot",
		})
	}
	return models, nil
}

func (p *Provider) Complete(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	var content strings.Builder
	var finish string
	for chunk := range ch {
		if chunk.Error != nil {
			return nil, chunk.Error
		}
		content.WriteString(chunk.Content)
		if chunk.FinishReason != "" {
			finish = chunk.FinishReason
		}
	}
	if finish == "" {
		finish = "stop"
	}
	return &provider.CompletionResponse{
		Content: content.String(), FinishReason: finish, Model: req.Model,
	}, nil
}

func (p *Provider) Stream(ctx context.Context, req *provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	token, err := p.getCopilotToken()
	if err != nil {
		return nil, err
	}

	// Build OpenAI-format payload (Copilot API is already OpenAI-compatible)
	var msgs []map[string]any
	for _, m := range req.Messages {
		msgs = append(msgs, map[string]any{"role": m.Role, "content": m.Content})
	}
	payload := map[string]any{
		"model":    req.Model,
		"messages": msgs,
		"stream":   true,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}

	body, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", copilotChatURL, bytes.NewReader(body))
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Copilot-Integration-Id", "vscode-chat")
	httpReq.Header.Set("Editor-Version", "vscode/1.100.0")
	httpReq.Header.Set("User-Agent", "GitHubCopilotChat/0.27.0")
	httpReq.Header.Set("Openai-Intent", "conversation-panel")

	resp, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("copilot request: %w", err)
	}
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("copilot: status %d: %s", resp.StatusCode, respBody)
	}

	ch := make(chan provider.StreamChunk, 64)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- provider.StreamChunk{FinishReason: "stop"}
				return
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) > 0 {
				c := chunk.Choices[0]
				if c.Delta.Content != "" {
					ch <- provider.StreamChunk{Content: c.Delta.Content}
				}
				if c.FinishReason != nil {
					ch <- provider.StreamChunk{FinishReason: *c.FinishReason}
				}
			}
		}
	}()

	return ch, nil
}

func (p *Provider) saveGitHubToken() {
	dir := filepath.Dir(p.tokenPath)
	os.MkdirAll(dir, 0700)
	os.WriteFile(p.tokenPath, []byte(p.githubToken), 0600)
	log.Printf("[copilot] GitHub token saved to %s", p.tokenPath)
}
