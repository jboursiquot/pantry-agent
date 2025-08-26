package ollama

import (
	"strings"
)

// Message represents an Ollama-specific message that supports tool results
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"` // Tool name for tool messages
}

// Prompt represents a prompt structure compatible with Ollama's native tool calling
type Prompt struct {
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}

// HasToolResult returns true if a tool result for the specified tool name exists in the prompt's message history.
// For Ollama native tool calling, it checks for messages with role "tool" and a matching "name" field.
func (op *Prompt) HasToolResult(tool string) bool {
	for _, msg := range op.Messages {
		if msg.Role == "tool" && msg.Name == tool {
			return true
		}
	}
	return false
}

// HasToolResultInContent returns true if a tool result for the specified tool name exists in any message content.
// This checks for tool results embedded in message content as JSON strings.
func (op *Prompt) HasToolResultInContent(tool string) bool {
	for _, msg := range op.Messages {
		// Check if the content contains a tool result JSON string
		if strings.Contains(msg.Content, `"tool_result":"`+tool+`"`) {
			return true
		}
	}
	return false
}

// Tool represents a tool in Ollama's native format
type Tool struct {
	Type     string     `json:"type"`
	Function ToolSchema `json:"function"`
}

// ToolSchema represents the function schema for Ollama tools
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Response represents the response structure from Ollama's API
type Response struct {
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a tool call made by the model
type ToolCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}
