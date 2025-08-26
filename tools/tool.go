package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/jsonschema"
)

type Tool interface {
	Name() string
	Title() string
	Description() string
	InputSchema() *jsonschema.Schema
	OutputSchema() *jsonschema.Schema
	Run(ctx context.Context, input map[string]any) (output map[string]any, err error)
}

type Call struct {
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
}
