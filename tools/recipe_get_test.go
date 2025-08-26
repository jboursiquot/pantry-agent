package tools

import (
	"context"
	"encoding/json"
	"testing"

	"pantryagent/tools/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecipeGet_Run(t *testing.T) {
	// Sample recipe data for testing
	testRecipes := []map[string]any{
		{
			"id":         "recipe1",
			"name":       "Scrambled Eggs",
			"meal_types": []any{"breakfast"},
			"ingredients": []any{
				map[string]any{"name": "egg", "qty": 2.0, "unit": "count"},
				map[string]any{"name": "milk", "qty": 50.0, "unit": "mL"},
			},
			"servings": 1.0,
		},
		{
			"id":         "recipe2",
			"name":       "Chicken Rice Bowl",
			"meal_types": []any{"lunch", "dinner"},
			"ingredients": []any{
				map[string]any{"name": "chicken", "qty": 200.0, "unit": "g"},
				map[string]any{"name": "rice", "qty": 100.0, "unit": "g"},
			},
			"servings": 2.0,
		},
		{
			"id":         "recipe3",
			"name":       "Toast",
			"meal_types": []any{"breakfast", "snack"},
			"ingredients": []any{
				map[string]any{"name": "bread", "qty": 2.0, "unit": "slice"},
			},
			"servings": 1.0,
		},
	}

	tests := []struct {
		name           string
		input          map[string]any
		expectedResult map[string]any
	}{
		{
			name:  "no meal type filter - return all recipes",
			input: map[string]any{},
			expectedResult: map[string]any{
				"recipes": testRecipes,
			},
		},
		{
			name: "empty meal types array - return all recipes",
			input: map[string]any{
				"meal_types": []any{},
			},
			expectedResult: map[string]any{
				"recipes": testRecipes,
			},
		},
		{
			name: "filter by breakfast",
			input: map[string]any{
				"meal_types": []any{"breakfast"},
			},
			expectedResult: map[string]any{
				"recipes": []map[string]any{
					testRecipes[0], // Scrambled Eggs
					testRecipes[2], // Toast
				},
			},
		},
		{
			name: "filter by lunch",
			input: map[string]any{
				"meal_types": []any{"lunch"},
			},
			expectedResult: map[string]any{
				"recipes": []map[string]any{
					testRecipes[1], // Chicken Rice Bowl
				},
			},
		},
		{
			name: "filter by multiple meal types",
			input: map[string]any{
				"meal_types": []any{"breakfast", "snack"},
			},
			expectedResult: map[string]any{
				"recipes": []map[string]any{
					testRecipes[0], // Scrambled Eggs (breakfast)
					testRecipes[2], // Toast (breakfast, snack)
				},
			},
		},
		{
			name: "filter by non-existent meal type",
			input: map[string]any{
				"meal_types": []any{"dessert"},
			},
			expectedResult: map[string]any{
				"recipes": []map[string]any{},
			},
		},
	}

	// Serialize the test recipes to JSON once
	recipeData, err := json.Marshal(testRecipes)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test recipes state for this test
			testState := storage.NewTestRecipeState(recipeData)

			// Create the tool instance
			tool := NewRecipeGet(testState)

			// Run the test
			result, err := tool.Run(context.Background(), tt.input)
			require.NoError(t, err)

			// Compare results
			assert.Equal(t, tt.expectedResult, result)
		})
	}

	t.Run("empty recipe list", func(t *testing.T) {
		// Serialize empty recipes to JSON
		recipeData, err := json.Marshal([]map[string]any{})
		require.NoError(t, err)

		// Create a test recipes state
		testState := storage.NewTestRecipeState(recipeData)

		// Create the tool instance
		tool := NewRecipeGet(testState)

		// Run the test
		result, err := tool.Run(context.Background(), map[string]any{})
		require.NoError(t, err)

		// Compare results
		expectedResult := map[string]any{
			"recipes": []map[string]any{},
		}
		assert.Equal(t, expectedResult, result)
	})

	t.Run("missing recipe data", func(t *testing.T) {
		// Create test state that returns an error
		testState := storage.NewTestRecipeStateWithError()
		tool := NewRecipeGet(testState)

		_, err := tool.Run(context.Background(), map[string]any{})
		assert.Error(t, err, "Expected error for missing recipe data")
		assert.Contains(t, err.Error(), "read recipes")
	})

	t.Run("corrupted recipe data", func(t *testing.T) {
		// Create test state with invalid JSON
		testState := storage.NewTestRecipeState([]byte("invalid json"))

		tool := NewRecipeGet(testState)

		_, err := tool.Run(context.Background(), map[string]any{})
		assert.Error(t, err, "Expected error for corrupted recipe data")
		assert.Contains(t, err.Error(), "parse recipes")
	})
}

func TestRecipeGet_ToolMethods(t *testing.T) {
	testState := storage.NewTestRecipeState([]byte("[]"))
	tool := NewRecipeGet(testState)

	t.Run("tool metadata", func(t *testing.T) {
		assert.Equal(t, "recipe_get", tool.Name())
		assert.Equal(t, "Get Recipes", tool.Title())
		assert.NotEmpty(t, tool.Description())
		assert.Contains(t, tool.Description(), "meal types")
	})

	t.Run("schemas are valid", func(t *testing.T) {
		inputSchema := tool.InputSchema()
		assert.NotNil(t, inputSchema)
		assert.Equal(t, "object", inputSchema.Type)

		// Check that meal_types is in the schema
		assert.Contains(t, inputSchema.Properties, "meal_types")
		mealTypesSchema := inputSchema.Properties["meal_types"]
		assert.Equal(t, "array", mealTypesSchema.Type)
		assert.NotNil(t, mealTypesSchema.Items)
		assert.Equal(t, "string", mealTypesSchema.Items.Type)

		outputSchema := tool.OutputSchema()
		assert.NotNil(t, outputSchema)
		assert.Equal(t, "object", outputSchema.Type)

		// Check output schema structure
		assert.Contains(t, outputSchema.Properties, "recipes")
		recipesSchema := outputSchema.Properties["recipes"]
		assert.Equal(t, "array", recipesSchema.Type)
		assert.NotNil(t, recipesSchema.Items)
		assert.Equal(t, "object", recipesSchema.Items.Type)

		// Check required fields
		assert.Contains(t, outputSchema.Required, "recipes")
	})
}
