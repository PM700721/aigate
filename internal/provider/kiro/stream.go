package kiro

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StreamEvent represents a parsed event from Kiro's binary stream.
type StreamEvent struct {
	Type    string // "content", "tool_start", "tool_input", "tool_stop", "usage"
	Content string
	Data    map[string]any
}

// StreamParser parses Kiro's AWS-style event stream.
type StreamParser struct {
	buffer      string
	lastContent string
}

// eventPatterns maps JSON prefixes to event types.
var eventPatterns = []struct {
	prefix    string
	eventType string
}{
	{`{"content":`, "content"},
	{`{"name":`, "tool_start"},
	{`{"input":`, "tool_input"},
	{`{"stop":`, "tool_stop"},
	{`{"followupPrompt":`, "followup"},
	{`{"usage":`, "usage"},
	{`{"contextUsagePercentage":`, "context_usage"},
}

// Feed adds a chunk to the buffer and returns parsed events.
func (p *StreamParser) Feed(chunk []byte) []StreamEvent {
	p.buffer += string(chunk)

	var events []StreamEvent
	for {
		earliestPos := -1
		var earliestType string

		for _, pat := range eventPatterns {
			pos := strings.Index(p.buffer, pat.prefix)
			if pos != -1 && (earliestPos == -1 || pos < earliestPos) {
				earliestPos = pos
				earliestType = pat.eventType
			}
		}

		if earliestPos == -1 {
			break
		}

		jsonEnd := findMatchingBrace(p.buffer, earliestPos)
		if jsonEnd == -1 {
			break // incomplete JSON, wait for more data
		}

		jsonStr := p.buffer[earliestPos : jsonEnd+1]
		p.buffer = p.buffer[jsonEnd+1:]

		var data map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
			continue
		}

		if ev := p.processEvent(data, earliestType); ev != nil {
			events = append(events, *ev)
		}
	}

	return events
}

func (p *StreamParser) processEvent(data map[string]any, eventType string) *StreamEvent {
	switch eventType {
	case "content":
		content, _ := data["content"].(string)
		if content == "" || content == p.lastContent {
			return nil
		}
		if _, ok := data["followupPrompt"]; ok {
			return nil
		}
		p.lastContent = content
		return &StreamEvent{Type: "content", Content: content}

	case "usage":
		return &StreamEvent{Type: "usage", Data: data}

	case "context_usage":
		return &StreamEvent{Type: "context_usage", Data: data}

	default:
		return nil // tool events handled later
	}
}

// findMatchingBrace finds the closing } for the { at startPos.
func findMatchingBrace(s string, startPos int) int {
	if startPos >= len(s) || s[startPos] != '{' {
		return -1
	}
	depth := 0
	inString := false
	escape := false

	for i := startPos; i < len(s); i++ {
		ch := s[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' && inString {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// FormatSSEChunk formats a single SSE chunk in OpenAI format.
func FormatSSEChunk(id, model, content string, finishReason *string) string {
	delta := map[string]any{}
	if content != "" {
		delta["content"] = content
	}

	choice := map[string]any{
		"index": 0,
		"delta": delta,
	}
	if finishReason != nil {
		choice["finish_reason"] = *finishReason
	}

	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"model":   model,
		"choices": []any{choice},
	}

	b, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", b)
}

// FormatSSEDone returns the SSE stream terminator.
func FormatSSEDone() string {
	return "data: [DONE]\n\n"
}
