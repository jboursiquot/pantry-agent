package mock

import (
	"context"
	"encoding/json"
	"testing"

	"pantryagent"
	"pantryagent/tools"
	"pantryagent/tools/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockLLMClient_Invoke(t *testing.T) {
	llm := NewLLMClient(Prompt{})

	t.Run("phase 1: no tool results - returns tool calls", func(t *testing.T) {
		// Create a basic prompt without any tool results
		ps := storage.NewTestPantryState([]byte(`{"ingredients":[]}`))
		rs := storage.NewTestRecipeState([]byte("[]"))
		registry, err := tools.NewRegistry(ps, rs)
		require.NoError(t, err)

		prompt, err := NewPrompt("Plan meals", registry)
		require.NoError(t, err)

		ctx := context.Background()
		response, err := llm.Invoke(ctx, prompt)

		require.NoError(t, err)
		assert.NotEmpty(t, response.Content, "Should have content with tool calls")

		// Parse the content to extract tool calls
		err = response.ParseModelOutput()
		require.NoError(t, err)

		assert.Len(t, response.ToolCalls, 2, "Should have 2 tool calls")

		// Verify tool names
		toolNames := make(map[string]bool)
		for _, call := range response.ToolCalls {
			toolNames[call.Name] = true
		}
		assert.True(t, toolNames["pantry_get"], "Should include pantry_get tool call")
		assert.True(t, toolNames["recipe_get"], "Should include recipe_get tool call")

		// Verify tool inputs
		for _, call := range response.ToolCalls {
			if call.Name == "pantry_get" {
				assert.Contains(t, call.Input, "current_day", "pantry_get should have current_day input")
				assert.Equal(t, 0, int(call.Input["current_day"].(float64)), "current_day should be 0")
			}
			if call.Name == "recipe_get" {
				assert.Contains(t, call.Input, "meal_types", "recipe_get should have meal_types input")
				mealTypes := call.Input["meal_types"].([]interface{})
				assert.Contains(t, mealTypes, "dinner", "Should request dinner recipes")
			}
		}
	})

	t.Run("phase 2: with tool results - returns final meal plan", func(t *testing.T) {
		// Create a prompt with tool results already present
		ps := storage.NewTestPantryState([]byte(`{"ingredients":[]}`))
		rs := storage.NewTestRecipeState([]byte("[]"))
		registry, err := tools.NewRegistry(ps, rs)
		require.NoError(t, err)

		prompt, err := NewPrompt("Plan meals", registry)
		require.NoError(t, err)

		// Add mock tool results to simulate having called tools already
		prompt.Messages = append(prompt.Messages, Message{
			Role: "user",
			Content: []MessagePart{{
				Type: "text",
				Text: `{"tool_result":"pantry_get","data":{"pantry":{"ingredients":[]}}}`,
			}},
		})
		prompt.Messages = append(prompt.Messages, Message{
			Role: "user",
			Content: []MessagePart{{
				Type: "text",
				Text: `{"tool_result":"recipe_get","data":{"recipes":[]}}`,
			}},
		})

		ctx := context.Background()
		response, err := llm.Invoke(ctx, prompt)

		require.NoError(t, err)
		assert.NotEmpty(t, response.Content, "Should have final meal plan content")

		// Parse as JSON meal plan
		var mealPlan pantryagent.MealPlan
		err = json.Unmarshal([]byte(response.Content), &mealPlan)
		require.NoError(t, err, "Response should be valid meal plan JSON")

		// Verify meal plan structure
		assert.True(t, mealPlan.IsValid(), "Should be a valid meal plan")
		assert.NotEmpty(t, mealPlan.Summary, "Should have a summary")
		assert.Len(t, mealPlan.DaysPlanned, 1, "Should have 1 day planned")
		assert.Equal(t, 1, mealPlan.DaysPlanned[0].Day, "Should be day 1")
		assert.Len(t, mealPlan.DaysPlanned[0].Meals, 1, "Should have 1 meal")
		assert.Equal(t, "dinner_bean_chili", mealPlan.DaysPlanned[0].Meals[0].ID)
		assert.Equal(t, "Bean Chili", mealPlan.DaysPlanned[0].Meals[0].Name)
		assert.Equal(t, 2, mealPlan.DaysPlanned[0].Meals[0].Servings)
	})

	t.Run("phase 3: fallback - returns tool calls", func(t *testing.T) {
		// Create a prompt that has neither pantry_get nor recipe_get tool results
		ps := storage.NewTestPantryState([]byte(`{"ingredients":[]}`))
		rs := storage.NewTestRecipeState([]byte("[]"))
		registry, err := tools.NewRegistry(ps, rs)
		require.NoError(t, err)

		prompt, err := NewPrompt("Plan meals", registry)
		require.NoError(t, err)

		// Add some other message that doesn't contain the expected tool results
		prompt.Messages = append(prompt.Messages, Message{
			Role: "user",
			Content: []MessagePart{{
				Type: "text",
				Text: "Some random user message without tool results",
			}},
		})

		ctx := context.Background()
		response, err := llm.Invoke(ctx, prompt)

		require.NoError(t, err)
		assert.NotEmpty(t, response.Content, "Should have fallback content")

		// Should return tool calls (fallback behavior)
		err = response.ParseModelOutput()
		require.NoError(t, err)

		assert.Len(t, response.ToolCalls, 2, "Should have 2 tool calls in fallback")
	})
}

func TestNewLLMClient(t *testing.T) {
	llm := NewLLMClient(Prompt{})
	assert.NotNil(t, llm, "NewLLMClient should return a non-nil client")
	assert.IsType(t, &LLMClient{}, llm, "Should return correct type")
}
