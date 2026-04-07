package kiro

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// buildKiroPayload converts an OpenAI-style chat request into Kiro API format.
func buildKiroPayload(model string, messages []Message, profileARN string) (map[string]any, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages")
	}

	// Extract system prompt
	var systemPrompt string
	var chatMessages []Message
	for _, m := range messages {
		if m.Role == "system" || m.Role == "developer" {
			systemPrompt += extractText(m.Content) + "\n"
		} else {
			chatMessages = append(chatMessages, m)
		}
	}
	systemPrompt = strings.TrimSpace(systemPrompt)

	if len(chatMessages) == 0 {
		chatMessages = []Message{{Role: "user", Content: "Hello"}}
	}

	// Ensure alternating roles and first message is user
	chatMessages = normalizeMessages(chatMessages)

	// Build history (all except last)
	var history []map[string]any
	for i := 0; i < len(chatMessages)-1; i++ {
		msg := chatMessages[i]
		text := extractText(msg.Content)

		// Prepend system prompt to first user message
		if i == 0 && systemPrompt != "" && msg.Role == "user" {
			text = systemPrompt + "\n\n" + text
		}

		if msg.Role == "user" {
			history = append(history, map[string]any{
				"userInputMessage": map[string]any{"content": text},
			})
		} else {
			history = append(history, map[string]any{
				"assistantResponseMessage": map[string]any{"content": text},
			})
		}
	}

	// Current message (last one)
	last := chatMessages[len(chatMessages)-1]
	currentContent := extractText(last.Content)

	// If no history, prepend system prompt to current
	if len(history) == 0 && systemPrompt != "" {
		currentContent = systemPrompt + "\n\n" + currentContent
	}

	if currentContent == "" {
		currentContent = "Continue"
	}

	// If last message is assistant, push to history and use "Continue"
	if last.Role == "assistant" {
		history = append(history, map[string]any{
			"assistantResponseMessage": map[string]any{"content": currentContent},
		})
		currentContent = "Continue"
	}

	// Resolve model ID
	modelID := resolveModelID(model)

	userInputMessage := map[string]any{
		"content": currentContent,
		"modelId": modelID,
		"origin":  "AI_EDITOR",
	}

	payload := map[string]any{
		"conversationState": map[string]any{
			"chatTriggerType": "MANUAL",
			"conversationId": uuid.New().String(),
			"currentMessage": map[string]any{
				"userInputMessage": userInputMessage,
			},
		},
	}

	if len(history) > 0 {
		payload["conversationState"].(map[string]any)["history"] = history
	}

	if profileARN != "" {
		payload["profileArn"] = profileARN
	}

	return payload, nil
}

// Message is a chat message.
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func extractText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		if content == nil {
			return ""
		}
		b, _ := json.Marshal(content)
		return string(b)
	}
}

func normalizeMessages(msgs []Message) []Message {
	if len(msgs) == 0 {
		return msgs
	}

	// Ensure first message is user
	if msgs[0].Role != "user" {
		msgs = append([]Message{{Role: "user", Content: "Hello"}}, msgs...)
	}

	// Ensure alternating roles
	var result []Message
	for i, msg := range msgs {
		// Normalize unknown roles to user
		if msg.Role != "user" && msg.Role != "assistant" {
			msg.Role = "user"
		}

		if i > 0 && msg.Role == result[len(result)-1].Role {
			// Same role as previous — merge content
			prev := &result[len(result)-1]
			prevText := extractText(prev.Content)
			curText := extractText(msg.Content)
			prev.Content = prevText + "\n" + curText
		} else {
			result = append(result, msg)
		}
	}

	return result
}

// resolveModelID maps user-facing model names to Kiro internal IDs.
func resolveModelID(model string) string {
	// Normalize: replace hyphens with dots for version numbers
	normalized := strings.ToLower(strings.TrimSpace(model))

	modelMap := map[string]string{
		"claude-sonnet-4-5":   "claude-sonnet-4.5",
		"claude-sonnet-4.5":   "claude-sonnet-4.5",
		"claude-sonnet-4":     "claude-sonnet-4",
		"claude-haiku-4-5":    "claude-haiku-4.5",
		"claude-haiku-4.5":    "claude-haiku-4.5",
		"claude-opus-4-5":     "claude-opus-4.5",
		"claude-opus-4.5":     "claude-opus-4.5",
		"claude-3-7-sonnet":   "claude-3.7-sonnet",
		"claude-3.7-sonnet":   "claude-3.7-sonnet",
		"deepseek-v3-2":       "deepseek-v3.2",
		"deepseek-v3.2":       "deepseek-v3.2",
		"minimax-m2-1":        "minimax-m2.1",
		"minimax-m2.1":        "minimax-m2.1",
		"qwen3-coder-next":    "qwen3-coder-next",
	}

	if mapped, ok := modelMap[normalized]; ok {
		return mapped
	}

	// Strip version suffixes like -20250929
	for prefix, id := range modelMap {
		if strings.HasPrefix(normalized, prefix) {
			return id
		}
	}

	return model // pass through as-is
}
