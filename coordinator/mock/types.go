package mock

import (
	"encoding/json"
	"pantryagent/tools"
	"strings"
)

type MessagePart struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
	Data      map[string]any `json:"data,omitempty"` // JSON result we want to feed back
}

type MessageParts []MessagePart

func (mp MessageParts) Join() string {
	var result string
	for _, part := range mp {
		if part.Type == "text" {
			result += part.Text
		}
	}
	return result
}

type Message struct {
	Role    string       `json:"role"`
	Content MessageParts `json:"content"`
}

type ToolResult struct {
	ToolUseID string
	ToolName  string
	Data      map[string]any
}

func NewToolResultMessage(results []ToolResult) Message {
	var parts MessageParts
	for _, result := range results {
		parts = append(parts, MessagePart{
			Type:      "tool_result",
			ToolUseID: result.ToolUseID,
			ToolName:  result.ToolName,
			Data:      result.Data,
		})
	}
	return Message{
		Role:    "user",
		Content: parts,
	}
}

// Response represents the model's response structure.
type Response struct {
	Content   string       `json:"content,omitempty"`
	ToolCalls []tools.Call `json:"tool_calls,omitempty"`
}

// ParseModelOutput parses model output text to extract both tool calls and remaining content.
// This method populates both the Content and ToolCalls fields based on what's found in the output.
// This is useful for LLMs that don't support formal tool calling but embed everything in their text response.
//
// The method handles several scenarios:
// 1. Pure tool calls: {"tool_calls":[{"name":"tool_name","input":{...}}, ...]}
// 2. Mixed content: "Some text\n{"tool_calls":[...]}\nMore text"
// 3. Pure content: Regular text or JSON without tool_calls
//
// Returns an error only if JSON parsing fails unexpectedly.
func (r *Response) ParseModelOutput() error {
	if r.Content == "" {
		return nil
	}

	s := strings.TrimSpace(r.Content)
	if s == "" {
		r.Content = ""
		r.ToolCalls = nil
		return nil
	}

	// Look for JSON objects that might contain tool calls
	var content strings.Builder
	var calls []tools.Call

	// Process the text character by character to find JSON objects
	i := 0
	for i < len(s) {
		// Find the start of a potential JSON object
		start := strings.IndexByte(s[i:], '{')
		if start == -1 {
			// No more JSON objects, append remaining text
			content.WriteString(s[i:])
			break
		}
		start += i

		// Add any text before the JSON object to content
		if start > i {
			content.WriteString(s[i:start])
		}

		// Find the matching closing brace
		braceCount := 0
		end := start
		inString := false
		escaped := false

		for end < len(s) {
			char := s[end]

			if escaped {
				escaped = false
				end++
				continue
			}

			if char == '\\' && inString {
				escaped = true
				end++
				continue
			}

			if char == '"' {
				inString = !inString
			} else if !inString {
				if char == '{' {
					braceCount++
				} else if char == '}' {
					braceCount--
					if braceCount == 0 {
						break
					}
				}
			}
			end++
		}

		if braceCount != 0 || end >= len(s) {
			// Malformed JSON, treat as regular content
			content.WriteString(s[start:])
			break
		}

		// Extract the JSON object
		jsonObj := s[start : end+1]

		// Try to parse it as tool calls
		var probe struct {
			ToolCalls []tools.Call `json:"tool_calls"`
		}

		if err := json.Unmarshal([]byte(jsonObj), &probe); err == nil && len(probe.ToolCalls) > 0 {
			// Found tool calls, add them to our collection
			for _, tc := range probe.ToolCalls {
				calls = append(calls, tools.Call{
					Name:  tc.Name,
					Input: tc.Input,
				})
			}
		} else {
			// Not tool calls, add to content
			content.WriteString(jsonObj)
		}

		// Move past this JSON object
		i = end + 1
	}

	// Set the parsed results
	r.Content = strings.TrimSpace(content.String())
	r.ToolCalls = calls

	return nil
}
