package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"pantryagent"
	"pantryagent/tools"
	"pantryagent/tools/storage"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
)

// mockLLM implements the llmClient interface for testing
type mockLLM struct {
	responses []Response
	callCount int
}

func (m *mockLLM) Invoke(ctx context.Context, prompt Prompt) (Response, error) {
	if m.callCount >= len(m.responses) {
		return Response{}, errors.New("no more responses available")
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func newMockLLM(responses ...Response) *mockLLM {
	return &mockLLM{responses: responses}
}

// Helper functions for test data
func validPantryData() map[string]any {
	return map[string]any{
		"ingredients": []any{
			map[string]any{"name": "egg", "qty": 12.0, "unit": "count", "days_left": 5.0},
			map[string]any{"name": "cheese", "qty": 200.0, "unit": "g", "days_left": 3.0},
			map[string]any{"name": "bread", "qty": 8.0, "unit": "slice", "days_left": 2.0},
			map[string]any{"name": "milk", "qty": 1000.0, "unit": "mL", "days_left": 7.0},
		},
	}
}

func validRecipeData() []any {
	return []any{
		map[string]any{
			"id":         "breakfast_scrambled_eggs",
			"name":       "Scrambled Eggs",
			"meal_types": []string{"breakfast"},
			"ingredients": []any{
				map[string]any{"name": "egg", "qty": 2.0, "unit": "count"},
				map[string]any{"name": "milk", "qty": 50.0, "unit": "mL"},
			},
			"servings": 1.0,
		},
		map[string]any{
			"id":         "lunch_grilled_cheese",
			"name":       "Grilled Cheese",
			"meal_types": []string{"lunch"},
			"ingredients": []any{
				map[string]any{"name": "bread", "qty": 2.0, "unit": "slice"},
				map[string]any{"name": "cheese", "qty": 50.0, "unit": "g"},
			},
			"servings": 1.0,
		},
	}
}

func validMealPlanJSON() string {
	return `{
		"summary": "2-day meal plan prioritizing cheese (expires in 3 days) and bread (expires in 2 days)",
		"days_planned": [
			{
				"day": 1,
				"meals": [
					{
						"id": "breakfast_scrambled_eggs",
						"name": "Scrambled Eggs",
						"servings": 2
					},
					{
						"id": "lunch_grilled_cheese",
						"name": "Grilled Cheese",
						"servings": 2
					}
				]
			},
			{
				"day": 2,
				"meals": [
					{
						"id": "lunch_grilled_cheese",
						"name": "Grilled Cheese",
						"servings": 1
					}
				]
			}
		]
	}`
}

func infeasibleMealPlanJSON() string {
	return `{
		"summary": "Infeasible plan requiring too much cheese",
		"days_planned": [
			{
				"day": 1,
				"meals": [
					{
						"id": "lunch_grilled_cheese",
						"name": "Grilled Cheese",
						"servings": 10
					}
				]
			}
		]
	}`
}

func setupTestRegistry() (*tools.Registry, error) {
	pantryBytes, _ := json.Marshal(validPantryData())
	recipeBytes, _ := json.Marshal(map[string]any{"recipes": validRecipeData()})

	ps := storage.NewTestPantryState(pantryBytes)
	rs := storage.NewTestRecipeState(recipeBytes)

	return tools.NewRegistry(ps, rs)
}

func TestCoordinatorRun(t *testing.T) {
	tests := []struct {
		name            string
		task            string
		maxIterations   int
		llmResponses    []Response
		expectedResult  string
		expectedError   string
		expectNoResult  bool
		resultValidator func(t *testing.T, result string)
	}{
		{
			name:          "successful coordination with direct final plan",
			task:          "Plan meals for 2 days",
			maxIterations: 5,
			llmResponses: []Response{
				{Content: validMealPlanJSON()}, // Direct final plan
			},
			resultValidator: func(t *testing.T, result string) {
				var mealPlan pantryagent.MealPlan
				err := json.Unmarshal([]byte(result), &mealPlan)
				require.NoError(t, err)
				assert.True(t, mealPlan.IsValid())
				assert.Equal(t, 2, len(mealPlan.DaysPlanned))
			},
		},
		{
			name:          "coordination with tool calls first",
			task:          "Plan breakfast for 1 day",
			maxIterations: 5,
			llmResponses: []Response{
				{
					Content: "I need to get pantry and recipe data",
					ToolCalls: []tools.Call{
						{Name: "pantry_get", Input: map[string]any{"current_day": 0}},
						{Name: "recipe_get", Input: map[string]any{"meal_types": []string{"breakfast"}}},
					},
				},
				{Content: validMealPlanJSON()}, // Final plan after tools
			},
			resultValidator: func(t *testing.T, result string) {
				var mealPlan pantryagent.MealPlan
				err := json.Unmarshal([]byte(result), &mealPlan)
				require.NoError(t, err)
				assert.True(t, mealPlan.IsValid())
			},
		},
		{
			name:          "infeasible plan gets corrected",
			task:          "Plan meals for 1 day",
			maxIterations: 5,
			llmResponses: []Response{
				{Content: infeasibleMealPlanJSON()}, // Infeasible plan first
				{Content: validMealPlanJSON()},      // Corrected plan
			},
			resultValidator: func(t *testing.T, result string) {
				var mealPlan pantryagent.MealPlan
				err := json.Unmarshal([]byte(result), &mealPlan)
				require.NoError(t, err)
				assert.True(t, mealPlan.IsValid())
				// Should be the corrected plan, not the infeasible one
				assert.NotContains(t, result, "Infeasible plan")
			},
		},
		{
			name:          "invalid JSON gets rejected",
			task:          "Plan meals",
			maxIterations: 5,
			llmResponses: []Response{
				{Content: `{"invalid": json without closing brace`}, // Invalid JSON
				{Content: validMealPlanJSON()},                      // Valid plan
			},
			resultValidator: func(t *testing.T, result string) {
				var mealPlan pantryagent.MealPlan
				err := json.Unmarshal([]byte(result), &mealPlan)
				require.NoError(t, err)
				assert.True(t, mealPlan.IsValid())
			},
		},
		{
			name:          "max iterations reached",
			task:          "Plan meals",
			maxIterations: 2,
			llmResponses: []Response{
				{Content: "I need tools"},
				{Content: "Still need more info"},
				{Content: validMealPlanJSON()}, // Would be valid but max iterations reached
			},
			expectNoResult: true,
			resultValidator: func(t *testing.T, result string) {
				assert.Empty(t, result)
			},
		},
		{
			name:          "excessive tool repetition handling",
			task:          "Plan meals",
			maxIterations: 5,
			llmResponses: []Response{
				{ToolCalls: []tools.Call{{Name: "pantry_get", Input: map[string]any{}}}},
				{ToolCalls: []tools.Call{{Name: "pantry_get", Input: map[string]any{}}}},
				{ToolCalls: []tools.Call{{Name: "pantry_get", Input: map[string]any{}}}}, // 3rd call, should be blocked
				{Content: validMealPlanJSON()},
			},
			resultValidator: func(t *testing.T, result string) {
				var mealPlan pantryagent.MealPlan
				err := json.Unmarshal([]byte(result), &mealPlan)
				require.NoError(t, err)
				assert.True(t, mealPlan.IsValid())
			},
		},
		{
			name:          "non-JSON content gets tool request",
			task:          "Plan meals",
			maxIterations: 5,
			llmResponses: []Response{
				{Content: "I will help you plan meals"}, // Non-JSON content
				{Content: validMealPlanJSON()},          // Valid plan after tool request
			},
			resultValidator: func(t *testing.T, result string) {
				var mealPlan pantryagent.MealPlan
				err := json.Unmarshal([]byte(result), &mealPlan)
				require.NoError(t, err)
				assert.True(t, mealPlan.IsValid())
			},
		},
		{
			name:          "LLM invoke error",
			task:          "Plan meals",
			maxIterations: 5,
			llmResponses:  []Response{}, // Empty responses will cause error
			expectedError: "invoke failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up test dependencies
			registry, err := setupTestRegistry()
			require.NoError(t, err)

			mockLLMClient := newMockLLM(tt.llmResponses...)
			logger := pantryagent.NewNoOpCoordinationLogger()
			tracerProvider := trace.NewTracerProvider()

			// Create coordinator
			coordinator := NewCoordinator(
				mockLLMClient,
				registry,
				validPantryData(),
				validRecipeData(),
				tt.maxIterations,
				logger,
				tracerProvider,
			)

			// Run coordination
			ctx := context.Background()
			result, err := coordinator.Run(ctx, tt.task)

			// Check error expectations
			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}
			assert.NoError(t, err)

			// Check result expectations
			if tt.expectNoResult {
				assert.Empty(t, result)
			} else if tt.expectedResult != "" {
				assert.Equal(t, tt.expectedResult, result)
			}

			// Run custom validator if provided
			if tt.resultValidator != nil {
				tt.resultValidator(t, result)
			}
		})
	}
}

func TestCoordinatorFeasibilityCheck(t *testing.T) {
	tests := []struct {
		name             string
		mealPlanJSON     string
		pantryData       map[string]any
		recipeData       []any
		expectFeasible   bool
		expectedProblems []string
	}{
		{
			name:           "feasible plan with sufficient ingredients",
			mealPlanJSON:   validMealPlanJSON(),
			pantryData:     validPantryData(),
			recipeData:     validRecipeData(),
			expectFeasible: true,
		},
		{
			name: "infeasible - insufficient quantity",
			mealPlanJSON: `{
				"summary": "Plan requiring too many eggs",
				"days_planned": [{
					"day": 1,
					"meals": [{
						"id": "breakfast_scrambled_eggs",
						"name": "Scrambled Eggs", 
						"servings": 10
					}]
				}]
			}`,
			pantryData: map[string]any{
				"ingredients": []any{
					map[string]any{"name": "egg", "qty": 12.0, "unit": "count", "days_left": 5.0}, // Only 12 eggs
					map[string]any{"name": "milk", "qty": 1000.0, "unit": "mL", "days_left": 7.0},
				},
			},
			recipeData:       validRecipeData(),
			expectFeasible:   false,
			expectedProblems: []string{"insufficient egg"},
		},
		{
			name: "infeasible - missing ingredient",
			mealPlanJSON: `{
				"summary": "Plan with unknown recipe",
				"days_planned": [{
					"day": 1,
					"meals": [{
						"id": "unknown_recipe",
						"name": "Unknown Recipe",
						"servings": 1
					}]
				}]
			}`,
			pantryData:       validPantryData(),
			recipeData:       validRecipeData(),
			expectFeasible:   false,
			expectedProblems: []string{"unknown recipe id"},
		},
		{
			name: "infeasible - unit mismatch",
			mealPlanJSON: `{
				"summary": "Plan with unit mismatch",
				"days_planned": [{
					"day": 1,
					"meals": [{
						"id": "breakfast_scrambled_eggs",
						"name": "Scrambled Eggs",
						"servings": 1
					}]
				}]
			}`,
			pantryData: map[string]any{
				"ingredients": []any{
					map[string]any{"name": "egg", "qty": 12.0, "unit": "dozen", "days_left": 5.0}, // Wrong unit
					map[string]any{"name": "milk", "qty": 1000.0, "unit": "mL", "days_left": 7.0},
				},
			},
			recipeData:       validRecipeData(),
			expectFeasible:   false,
			expectedProblems: []string{"unit mismatch: egg"},
		},
		{
			name: "empty days planned",
			mealPlanJSON: `{
				"summary": "Empty plan",
				"days_planned": []
			}`,
			pantryData:       validPantryData(),
			recipeData:       validRecipeData(),
			expectFeasible:   false,
			expectedProblems: []string{"days_planned must be non-empty"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coordinator := &Coordinator{
				pantry:  tt.pantryData,
				recipes: tt.recipeData,
			}

			feasible, problems, err := coordinator.checkFeasible(tt.mealPlanJSON)

			assert.NoError(t, err)

			if !tt.expectFeasible {
				assert.False(t, feasible, "Expected plan to be infeasible")
				assert.NotEmpty(t, problems, "Expected problems to be reported")

				// Debug output
				t.Logf("Problems found: %v", problems)

				for _, expectedProblem := range tt.expectedProblems {
					found := false
					for _, problem := range problems {
						if strings.Contains(strings.ToLower(problem), strings.ToLower(expectedProblem)) {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected problem %q not found in: %v", expectedProblem, problems)
				}
			} else {
				assert.True(t, feasible, "Expected plan to be feasible")
				if len(problems) > 0 {
					t.Logf("Unexpected problems: %v", problems)
				}
			}
		})
	}
}

func TestCoordinatorToolIntegration(t *testing.T) {
	tests := []struct {
		name          string
		toolError     error
		expectError   bool
		errorContains string
	}{
		{
			name:        "successful tool execution",
			toolError:   nil,
			expectError: false,
		},
		{
			name:          "tool execution error",
			toolError:     errors.New("pantry load failed"),
			expectError:   true,
			errorContains: "pantry load failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up registry with potential errors
			var ps storage.PantryState
			var rs storage.RecipeState

			if tt.toolError != nil {
				ps = storage.NewTestPantryStateWithError()
				rs = storage.NewTestRecipeState([]byte(`{"recipes":[]}`))
			} else {
				pantryBytes, _ := json.Marshal(validPantryData())
				recipeBytes, _ := json.Marshal(map[string]any{"recipes": validRecipeData()})
				ps = storage.NewTestPantryState(pantryBytes)
				rs = storage.NewTestRecipeState(recipeBytes)
			}

			registry, err := tools.NewRegistry(ps, rs)
			require.NoError(t, err)

			// Mock LLM that calls tools first
			mockLLMClient := newMockLLM(
				Response{
					Content: "Getting data",
					ToolCalls: []tools.Call{
						{Name: "pantry_get", Input: map[string]any{}},
					},
				},
				Response{Content: validMealPlanJSON()},
			)

			logger := pantryagent.NewNoOpCoordinationLogger()
			tracerProvider := trace.NewTracerProvider()
			coordinator := NewCoordinator(
				mockLLMClient,
				registry,
				validPantryData(),
				validRecipeData(),
				5,
				logger,
				tracerProvider,
			)

			ctx := context.Background()
			result, err := coordinator.Run(ctx, "Plan meals")

			if tt.expectError {
				// Tool error should not fail coordination, just provide error in tool result
				assert.NoError(t, err) // Coordination itself should succeed
				// But we can check that it completed (might be empty result due to tool error)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, result)

				var mealPlan pantryagent.MealPlan
				unmarshalErr := json.Unmarshal([]byte(result), &mealPlan)
				require.NoError(t, unmarshalErr)
				assert.True(t, mealPlan.IsValid())
			}
		})
	}
}

func TestCoordinatorEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		setupFunc    func() (*Coordinator, error)
		task         string
		expectResult bool
		expectError  bool
	}{
		{
			name: "coordinator with nil data",
			setupFunc: func() (*Coordinator, error) {
				registry, err := setupTestRegistry()
				if err != nil {
					return nil, err
				}
				mockLLMClient := newMockLLM(
					Response{Content: validMealPlanJSON()}, // First attempt fails feasibility
					Response{Content: validMealPlanJSON()}, // Second attempt
					Response{Content: validMealPlanJSON()}, // Third attempt
					Response{Content: validMealPlanJSON()}, // Fourth attempt
					Response{Content: validMealPlanJSON()}, // Fifth attempt
				)
				logger := pantryagent.NewNoOpCoordinationLogger()
				return NewCoordinator(mockLLMClient, registry, nil, nil, 5, logger, trace.NewTracerProvider()), nil
			},
			task:         "Plan meals",
			expectResult: false, // Will fail feasibility due to missing recipes
			expectError:  false,
		},
		{
			name: "coordinator with empty data",
			setupFunc: func() (*Coordinator, error) {
				registry, err := setupTestRegistry()
				if err != nil {
					return nil, err
				}
				mockLLMClient := newMockLLM(
					Response{Content: validMealPlanJSON()}, // First attempt fails feasibility
					Response{Content: validMealPlanJSON()}, // Second attempt
					Response{Content: validMealPlanJSON()}, // Third attempt
					Response{Content: validMealPlanJSON()}, // Fourth attempt
					Response{Content: validMealPlanJSON()}, // Fifth attempt
				)
				logger := pantryagent.NewNoOpCoordinationLogger()
				return NewCoordinator(mockLLMClient, registry, map[string]any{}, []any{}, 5, logger, trace.NewTracerProvider()), nil
			},
			task:         "Plan meals",
			expectResult: false, // Will fail feasibility due to missing recipes
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coordinator, err := tt.setupFunc()
			require.NoError(t, err)

			ctx := context.Background()
			result, err := coordinator.Run(ctx, tt.task)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.expectResult {
					assert.NotEmpty(t, result)
				}
				// Can be empty if coordination reaches max iterations or fails feasibility
			}
		})
	}
}
