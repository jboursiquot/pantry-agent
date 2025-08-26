package tools

import (
	"context"
	"encoding/json"
	"testing"

	"pantryagent/tools/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPantryGet_Run(t *testing.T) {
	tests := []struct {
		name           string
		initialPantry  Pantry
		input          map[string]any
		expectedResult map[string]any
	}{
		{
			name: "basic pantry with all ingredient types",
			initialPantry: Pantry{
				Ingredients: []Ingredient{
					{Name: "egg", Qty: 100, Unit: "count"},
					{Name: "milk", Qty: 2000, Unit: "mL", PerishableDays: 7, AddedDay: 1},
					{Name: "bread", Qty: 2, Unit: "loaf", DaysLeft: 3},
					{Name: "rice", Qty: 1000, Unit: "g"},
					{Name: "chicken", Qty: 2, Unit: "pound", PerishableDays: 3, AddedDay: 5},
				},
			},
			input: map[string]any{
				"current_day": 10.0,
			},
			expectedResult: map[string]any{
				"pantry": map[string]any{
					"ingredients": []any{
						map[string]any{
							"name":      "egg",
							"qty":       100.0,
							"unit":      "count",
							"days_left": 9999.0, // non-perishable default
						},
						map[string]any{
							"name":      "milk",
							"qty":       2000.0,
							"unit":      "mL",
							"days_left": -2.0, // expired 2 days ago
						},
						map[string]any{
							"name":      "bread",
							"qty":       2.0,
							"unit":      "loaf",
							"days_left": 3.0,
						},
						map[string]any{
							"name":      "rice",
							"qty":       1000.0,
							"unit":      "g",
							"days_left": 9999.0, // non-perishable default
						},
						map[string]any{
							"name":      "chicken",
							"qty":       2.0,
							"unit":      "pound",
							"days_left": -2.0, // expired 2 days ago
						},
					},
				},
			},
		},
		{
			name: "empty pantry",
			initialPantry: Pantry{
				Ingredients: []Ingredient{},
			},
			input: map[string]any{
				"current_day": 5.0,
			},
			expectedResult: map[string]any{
				"pantry": map[string]any{
					"ingredients": []any{},
				},
			},
		},
		{
			name: "no current_day parameter defaults to day 0",
			initialPantry: Pantry{
				Ingredients: []Ingredient{
					{Name: "tomato", Qty: 5, Unit: "pieces", PerishableDays: 7, AddedDay: 0},
				},
			},
			input: map[string]any{},
			expectedResult: map[string]any{
				"pantry": map[string]any{
					"ingredients": []any{
						map[string]any{
							"name":      "tomato",
							"qty":       5.0,
							"unit":      "pieces",
							"days_left": 7.0,
						},
					},
				},
			},
		},
		{
			name: "ingredients with various freshness states",
			initialPantry: Pantry{
				Ingredients: []Ingredient{
					{Name: "fresh_item", Qty: 1, Unit: "count", PerishableDays: 10, AddedDay: 5},   // days_left = 10
					{Name: "expiring_soon", Qty: 1, Unit: "count", PerishableDays: 3, AddedDay: 2}, // days_left = 0
					{Name: "expired_item", Qty: 1, Unit: "count", PerishableDays: 2, AddedDay: 1},  // days_left = -2
					{Name: "preset_days_left", Qty: 1, Unit: "count", DaysLeft: 5},                 // uses preset value
				},
			},
			input: map[string]any{
				"current_day": 5.0,
			},
			expectedResult: map[string]any{
				"pantry": map[string]any{
					"ingredients": []any{
						map[string]any{
							"name":      "fresh_item",
							"qty":       1.0,
							"unit":      "count",
							"days_left": 10.0,
						},
						map[string]any{
							"name":      "expiring_soon",
							"qty":       1.0,
							"unit":      "count",
							"days_left": 0.0,
						},
						map[string]any{
							"name":      "expired_item",
							"qty":       1.0,
							"unit":      "count",
							"days_left": -2.0,
						},
						map[string]any{
							"name":      "preset_days_left",
							"qty":       1.0,
							"unit":      "count",
							"days_left": 5.0,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize the pantry with test data
			pantryData, err := json.Marshal(tt.initialPantry)
			require.NoError(t, err)

			// Create a test pantry store for this test
			testStore := storage.NewTestPantryState(pantryData)

			// Create the tool instance
			tool := NewPantryGet(testStore)

			// Run the test
			result, err := tool.Run(context.Background(), tt.input)
			require.NoError(t, err)

			// Compare results
			assert.Equal(t, tt.expectedResult, result)
		})
	}

	t.Run("missing pantry data", func(t *testing.T) {
		// Create test store that returns an error
		testStore := storage.NewTestPantryStateWithError()
		tool := NewPantryGet(testStore)

		input := map[string]any{
			"current_day": 5.0,
		}

		_, err := tool.Run(context.Background(), input)
		assert.Error(t, err, "Expected error for missing pantry data")
	})

	t.Run("corrupted pantry data", func(t *testing.T) {
		// Create test store with invalid JSON
		testStore := storage.NewTestPantryState([]byte("invalid json"))

		tool := NewPantryGet(testStore)
		input := map[string]any{}

		_, err := tool.Run(context.Background(), input)
		assert.Error(t, err, "Expected error for corrupted pantry data")
	})
}

func TestPantryGet_ToolMethods(t *testing.T) {
	testStore := storage.NewTestPantryState([]byte("{}"))
	tool := NewPantryGet(testStore)

	t.Run("tool metadata", func(t *testing.T) {
		assert.Equal(t, "pantry_get", tool.Name())
		assert.Equal(t, "Get Pantry (with freshness)", tool.Title())
		assert.NotEmpty(t, tool.Description())
		assert.Contains(t, tool.Description(), "days_left")
	})

	t.Run("schemas are valid", func(t *testing.T) {
		inputSchema := tool.InputSchema()
		assert.NotNil(t, inputSchema)
		assert.Equal(t, "object", inputSchema.Type)

		// Check that current_day is in the schema
		assert.Contains(t, inputSchema.Properties, "current_day")
		assert.Equal(t, "integer", inputSchema.Properties["current_day"].Type)

		outputSchema := tool.OutputSchema()
		assert.NotNil(t, outputSchema)
		assert.Equal(t, "object", outputSchema.Type)

		// Check output schema structure
		assert.Contains(t, outputSchema.Properties, "pantry")
		pantrySchema := outputSchema.Properties["pantry"]
		assert.Equal(t, "object", pantrySchema.Type)
		assert.Contains(t, pantrySchema.Properties, "ingredients")
		ingredientsSchema := pantrySchema.Properties["ingredients"]
		assert.Equal(t, "array", ingredientsSchema.Type)
		assert.NotNil(t, ingredientsSchema.Items)

		// Check ingredient item schema
		itemProps := ingredientsSchema.Items.Properties
		assert.Contains(t, itemProps, "name")
		assert.Contains(t, itemProps, "qty")
		assert.Contains(t, itemProps, "unit")
		assert.Contains(t, itemProps, "days_left")
	})
}

// BenchmarkPantryGet_Run benchmarks the main Run function
func BenchmarkPantryGet_Run(b *testing.B) {
	// Setup test store with pantry data
	initialPantry := Pantry{
		Ingredients: []Ingredient{
			{Name: "egg", Qty: 100, Unit: "count"},
			{Name: "milk", Qty: 2000, Unit: "mL", PerishableDays: 7, AddedDay: 1},
			{Name: "bread", Qty: 2, Unit: "loaf", DaysLeft: 3},
			{Name: "rice", Qty: 1000, Unit: "g"},
			{Name: "chicken", Qty: 2, Unit: "pound", PerishableDays: 3, AddedDay: 5},
		},
	}
	pantryData, _ := json.Marshal(initialPantry)
	testStore := storage.NewTestPantryState(pantryData)

	tool := NewPantryGet(testStore)
	input := map[string]any{
		"current_day": 10.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tool.Run(context.Background(), input)
		require.NoError(b, err)
	}
}

func TestPantryGet_LargeQuantities(t *testing.T) {
	// Test with very large quantities to ensure no overflow issues
	initialPantry := Pantry{
		Ingredients: []Ingredient{
			{Name: "warehouse_rice", Qty: 999999.99, Unit: "kg"},
			{Name: "tiny_spice", Qty: 0.001, Unit: "g"},
		},
	}
	pantryData, _ := json.Marshal(initialPantry)
	testStore := storage.NewTestPantryState(pantryData)

	tool := NewPantryGet(testStore)
	result, err := tool.Run(context.Background(), map[string]any{})
	require.NoError(t, err)

	expected := map[string]any{
		"pantry": map[string]any{
			"ingredients": []any{
				map[string]any{
					"name":      "warehouse_rice",
					"qty":       999999.99,
					"unit":      "kg",
					"days_left": 9999.0, // non-perishable default
				},
				map[string]any{
					"name":      "tiny_spice",
					"qty":       0.001,
					"unit":      "g",
					"days_left": 9999.0, // non-perishable default
				},
			},
		},
	}

	assert.Equal(t, expected, result)
}
