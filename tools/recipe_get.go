package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonschema"

	"pantryagent/tools/storage"
)

type RecipeGet struct{ state storage.RecipeState }

func NewRecipeGet(state storage.RecipeState) *RecipeGet { return &RecipeGet{state: state} }

func (t *RecipeGet) Name() string  { return "recipe_get" }
func (t *RecipeGet) Title() string { return "Get Recipes" }
func (t *RecipeGet) Description() string {
	return "Gets recipes filtered by meal types (optional)."
}

func (t *RecipeGet) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"meal_types": {
				Type:  "array",
				Items: &jsonschema.Schema{Type: "string"},
			},
		},
	}
}

func (t *RecipeGet) OutputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"recipes": {
				Type: "array",
				Items: &jsonschema.Schema{
					Type: "object",
					// keep schema open to accept your recipe JSON as-is
				},
			},
		},
		Required: []string{"recipes"},
	}
}

func (t *RecipeGet) Run(ctx context.Context, input map[string]any) (map[string]any, error) {
	recipes, err := t.load(ctx)
	if err != nil {
		return nil, err
	}

	raw, _ := input["meal_types"].([]any)
	if len(raw) == 0 {
		return map[string]any{"recipes": recipes}, nil
	}

	// Build a set of desired meal types
	want := map[string]bool{}
	for _, v := range raw {
		if s, _ := v.(string); s != "" {
			want[s] = true
		}
	}

	// Filter recipes containing any of the requested meal types
	out := make([]map[string]any, 0)
	for _, rec := range recipes {
		if mealTypes, ok := rec["meal_types"].([]any); ok {
			include := false
			for _, m := range mealTypes {
				if s, _ := m.(string); want[s] {
					include = true
					break
				}
			}
			if include {
				out = append(out, rec)
			}
		}
	}

	return map[string]any{"recipes": out}, nil
}

func (t *RecipeGet) load(ctx context.Context) ([]map[string]any, error) {
	b, err := t.state.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("read recipes: %w", err)
	}
	var recipes []map[string]any
	if err := json.Unmarshal(b, &recipes); err != nil {
		return nil, fmt.Errorf("parse recipes: %w", err)
	}
	return recipes, nil
}
