package ollama

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pantryagent/tools"
	"pantryagent/tools/storage"
)

func TestPrompt_New(t *testing.T) {
	// Create test storage
	ps := storage.NewTestPantryState([]byte(`{"ingredients":[]}`))
	rs := storage.NewTestRecipeState([]byte("[]"))
	registry, err := tools.NewRegistry(ps, rs)
	require.NoError(t, err)

	// Create prompt
	prompt, err := NewPrompt("Plan meals for 2 days", registry)
	require.NoError(t, err)

	// Verify basic structure
	assert.Len(t, prompt.Messages, 2, "Should have system and user messages")
	assert.Equal(t, "system", prompt.Messages[0].Role)
	assert.Equal(t, "user", prompt.Messages[1].Role)
	assert.Equal(t, "Plan meals for 2 days", prompt.Messages[1].Content)

	// Verify tools are in Ollama format
	assert.Len(t, prompt.Tools, 2, "Should have 2 tools")

	// Check tool names
	toolNames := make(map[string]bool)
	for _, tool := range prompt.Tools {
		toolNames[tool.Function.Name] = true
		assert.Equal(t, "function", tool.Type, "Tool type should be 'function'")
		assert.NotEmpty(t, tool.Function.Description, "Tool should have description")
		assert.NotNil(t, tool.Function.Parameters, "Tool should have parameters")
	}

	assert.True(t, toolNames["pantry_get"], "Should have pantry_get tool")
	assert.True(t, toolNames["recipe_get"], "Should have recipe_get tool")

	// Verify pantry_get tool structure
	var pantryTool *Tool
	for i := range prompt.Tools {
		if prompt.Tools[i].Function.Name == "pantry_get" {
			pantryTool = &prompt.Tools[i]
			break
		}
	}
	require.NotNil(t, pantryTool, "Should find pantry_get tool")

	// Check parameters structure
	params := pantryTool.Function.Parameters
	assert.Equal(t, "object", params["type"])
	assert.NotNil(t, params["properties"])
	// Note: pantry_get tool has no required parameters, so required field may not be present

	// Verify the tool can be marshaled to the expected JSON format
	toolJSON, err := json.MarshalIndent(pantryTool, "", "  ")
	require.NoError(t, err)

	// Parse it back to verify structure
	var parsedTool map[string]interface{}
	err = json.Unmarshal(toolJSON, &parsedTool)
	require.NoError(t, err)

	assert.Equal(t, "function", parsedTool["type"])
	function := parsedTool["function"].(map[string]interface{})
	assert.Equal(t, "pantry_get", function["name"])
	assert.Contains(t, function["description"], "pantry")
}

func TestPrompt_HasToolResult(t *testing.T) {
	ps := storage.NewTestPantryState([]byte(`{"ingredients":[]}`))
	rs := storage.NewTestRecipeState([]byte("[]"))
	registry, err := tools.NewRegistry(ps, rs)
	require.NoError(t, err)

	prompt, err := NewPrompt("Plan meals", registry)
	require.NoError(t, err)

	t.Run("no tool results", func(t *testing.T) {
		assert.False(t, prompt.HasToolResult("pantry_get"))
		assert.False(t, prompt.HasToolResult("recipe_get"))
	})

	t.Run("with tool results", func(t *testing.T) {
		// Add a message with tool result (using Ollama's role:"tool" format)
		prompt.Messages = append(prompt.Messages, Message{
			Role:    "tool",
			Name:    "pantry_get",
			Content: `{"pantry":{"ingredients":[]}}`,
		})

		assert.True(t, prompt.HasToolResult("pantry_get"))
		assert.False(t, prompt.HasToolResult("recipe_get"))
	})
}
