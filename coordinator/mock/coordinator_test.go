package mock

import (
	"context"
	"encoding/json"
	"pantryagent"
	"strings"
	"testing"

	"pantryagent/tools"
	"pantryagent/tools/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockCoordinator(t *testing.T) {
	// Sample pantry data for testing
	pantryData := map[string]any{
		"ingredients": []map[string]any{
			{"name": "egg", "qty": 12.0, "unit": "count", "days_left": 5.0},
			{"name": "cheese", "qty": 200.0, "unit": "g", "days_left": 3.0},
			{"name": "bread", "qty": 1.0, "unit": "loaf", "days_left": 2.0},
		},
	}
	pantryDataBytes, _ := json.Marshal(pantryData)

	// Sample recipe data for testing
	recipeData := []map[string]any{
		{
			"id":         "dinner_scrambled_eggs",
			"name":       "Scrambled Eggs",
			"meal_types": []string{"dinner"},
			"ingredients": []map[string]any{
				{"name": "egg", "qty": 3.0, "unit": "count"},
				{"name": "cheese", "qty": 50.0, "unit": "g"},
			},
			"servings": 1.0,
		},
		{
			"id":         "dinner_grilled_cheese",
			"name":       "Grilled Cheese",
			"meal_types": []string{"dinner"},
			"ingredients": []map[string]any{
				{"name": "bread", "qty": 2.0, "unit": "slice"},
				{"name": "cheese", "qty": 100.0, "unit": "g"},
			},
			"servings": 1.0,
		},
	}
	recipeDataBytes, _ := json.Marshal(recipeData)

	tests := []struct {
		name                string
		task                string
		maxIterations       int
		expectError         bool
		expectResult        bool
		expectedResultCheck func(t *testing.T, result string)
	}{
		{
			name:          "successful coordination",
			task:          "Plan dinners for the next 2 days for 2 servings each",
			maxIterations: 5,
			expectError:   false,
			expectResult:  true,
			expectedResultCheck: func(t *testing.T, result string) {
				// Parse the result as JSON to validate structure
				var mealPlan pantryagent.MealPlan
				err := json.Unmarshal([]byte(result), &mealPlan)
				require.NoError(t, err, "Result should be valid JSON with MealPlan structure")

				// Validate the meal plan structure
				assert.True(t, mealPlan.IsValid(), "Meal plan should be valid")
				assert.NotEmpty(t, mealPlan.Summary, "Should have a summary")
				assert.NotEmpty(t, mealPlan.DaysPlanned, "Should have at least one day planned")

				// Check that it contains expected elements
				assert.Contains(t, strings.ToLower(mealPlan.Summary), "dinner", "Summary should mention dinner")
			},
		},
		{
			name:          "coordination with different task",
			task:          "Plan breakfast and lunch for 1 day",
			maxIterations: 3,
			expectError:   false,
			expectResult:  true,
			expectedResultCheck: func(t *testing.T, result string) {
				// Should still produce a valid meal plan even with different task wording
				var mealPlan pantryagent.MealPlan
				err := json.Unmarshal([]byte(result), &mealPlan)
				require.NoError(t, err)
				assert.True(t, mealPlan.IsValid())
			},
		},
		{
			name:          "max iterations limit",
			task:          "Plan meals",
			maxIterations: 1,
			expectError:   false,
			expectResult:  false,
			expectedResultCheck: func(t *testing.T, result string) {
				// With max iterations of 1, should not produce a complete result
				assert.Empty(t, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up test storage
			ps := storage.NewTestPantryState(pantryDataBytes)
			rs := storage.NewTestRecipeState(recipeDataBytes)

			// Create tool registry
			registry, err := tools.NewRegistry(ps, rs)
			require.NoError(t, err)

			// Create mock LLM
			llm := NewLLMClient(Prompt{})

			// Create no-op logger to avoid noise
			logger := pantryagent.NewNoOpCoordinationLogger()

			// Create coordinator
			coordinator := NewCoordinator(llm, registry, tt.maxIterations, logger)

			// Run the coordination
			ctx := context.Background()
			result, err := coordinator.Run(ctx, tt.task)

			// Check error expectation
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Check result expectations
			if tt.expectedResultCheck != nil {
				tt.expectedResultCheck(t, result)
			}
		})
	}
}

func TestMockCoordinatorWithErrorConditions(t *testing.T) {
	tests := []struct {
		name          string
		setupError    func() (*tools.Registry, llmClient)
		expectError   bool
		errorContains string
	}{
		{
			name: "pantry load error",
			setupError: func() (*tools.Registry, llmClient) {
				ps := storage.NewTestPantryStateWithError()
				rs := storage.NewTestRecipeState([]byte("[]"))
				registry, _ := tools.NewRegistry(ps, rs)
				return registry, NewLLMClient(Prompt{})
			},
			expectError:   true,
			errorContains: "failed to run tool",
		},
		{
			name: "recipe load error",
			setupError: func() (*tools.Registry, llmClient) {
				ps := storage.NewTestPantryState([]byte(`{"ingredients":[]}`))
				rs := storage.NewTestRecipeStateWithError()
				registry, _ := tools.NewRegistry(ps, rs)
				return registry, NewLLMClient(Prompt{})
			},
			expectError:   true,
			errorContains: "failed to run tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, llm := tt.setupError()
			logger := pantryagent.NewNoOpCoordinationLogger()
			coordinator := NewCoordinator(llm, registry, 5, logger)

			ctx := context.Background()
			_, err := coordinator.Run(ctx, "Plan meals")

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMockLLMBehavior(t *testing.T) {
	tests := []struct {
		name             string
		setupPrompt      func() Prompt
		expectedResponse func(t *testing.T, response Response)
	}{
		{
			name: "initial state - no tool results",
			setupPrompt: func() Prompt {
				// Empty tool registry for this test
				ps := storage.NewTestPantryState([]byte(`{"ingredients":[]}`))
				rs := storage.NewTestRecipeState([]byte("[]"))
				registry, _ := tools.NewRegistry(ps, rs)
				prompt, _ := NewPrompt("Plan meals", registry)
				return prompt
			},
			expectedResponse: func(t *testing.T, response Response) {
				// Should return tool calls for pantry_get and recipe_get
				assert.NotEmpty(t, response.Content, "Should have content")

				// Parse the tool calls from content
				err := response.ParseModelOutput()
				require.NoError(t, err)

				// Should contain tool calls
				assert.Len(t, response.ToolCalls, 2, "Should have 2 tool calls")

				toolNames := make([]string, len(response.ToolCalls))
				for i, call := range response.ToolCalls {
					toolNames[i] = call.Name
				}
				assert.Contains(t, toolNames, "pantry_get")
				assert.Contains(t, toolNames, "recipe_get")
			},
		},
		{
			name: "with tool results - should return final plan",
			setupPrompt: func() Prompt {
				ps := storage.NewTestPantryState([]byte(`{"ingredients":[]}`))
				rs := storage.NewTestRecipeState([]byte("[]"))
				registry, _ := tools.NewRegistry(ps, rs)
				prompt, _ := NewPrompt("Plan meals", registry)

				// Add tool results to simulate second phase
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

				return prompt
			},
			expectedResponse: func(t *testing.T, response Response) {
				// Should return final meal plan
				assert.NotEmpty(t, response.Content, "Should have final content")

				// Parse as meal plan
				var mealPlan pantryagent.MealPlan
				err := json.Unmarshal([]byte(response.Content), &mealPlan)
				require.NoError(t, err, "Should be valid meal plan JSON")
				assert.True(t, mealPlan.IsValid(), "Should be valid meal plan")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := NewLLMClient(Prompt{})
			prompt := tt.setupPrompt()

			ctx := context.Background()
			response, err := llm.Invoke(ctx, prompt)

			require.NoError(t, err)
			tt.expectedResponse(t, response)
		})
	}
}

func TestCoordinatorToolIntegration(t *testing.T) {
	// Test that the coordinator properly integrates with real tools
	pantryData := map[string]any{
		"ingredients": []map[string]any{
			{"name": "egg", "qty": 6.0, "unit": "count", "days_left": 5.0},
			{"name": "milk", "qty": 500.0, "unit": "mL", "days_left": 7.0},
		},
	}
	pantryDataBytes, _ := json.Marshal(pantryData)

	recipeData := []map[string]any{
		{
			"id":         "breakfast_scrambled_eggs",
			"name":       "Scrambled Eggs",
			"meal_types": []string{"breakfast"},
			"ingredients": []map[string]any{
				{"name": "egg", "qty": 2.0, "unit": "count"},
				{"name": "milk", "qty": 50.0, "unit": "mL"},
			},
			"servings": 1.0,
		},
	}
	recipeDataBytes, _ := json.Marshal(recipeData)

	ps := storage.NewTestPantryState(pantryDataBytes)
	rs := storage.NewTestRecipeState(recipeDataBytes)
	registry, err := tools.NewRegistry(ps, rs)
	require.NoError(t, err)

	llm := NewLLMClient(Prompt{})
	logger := pantryagent.NewNoOpCoordinationLogger()
	coordinator := NewCoordinator(llm, registry, 5, logger)

	ctx := context.Background()
	result, err := coordinator.Run(ctx, "Plan breakfast for 1 day for 1 serving")

	assert.NoError(t, err)
	assert.NotEmpty(t, result)

	// Validate the result is a proper meal plan
	var mealPlan pantryagent.MealPlan
	err = json.Unmarshal([]byte(result), &mealPlan)
	require.NoError(t, err)
	assert.True(t, mealPlan.IsValid())
}
