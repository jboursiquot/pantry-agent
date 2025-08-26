package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/jsonschema"

	"pantryagent/tools/storage"
)

type PantryGet struct{ state storage.PantryState }

func NewPantryGet(state storage.PantryState) *PantryGet { return &PantryGet{state: state} }

func (t *PantryGet) Name() string  { return "pantry_get" }
func (t *PantryGet) Title() string { return "Get Pantry (with freshness)" }
func (t *PantryGet) Description() string {
	return "Returns pantry quantities plus days_left for perishables at a given current_day."
}

func (t *PantryGet) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"current_day": {
				Type: "integer",
			},
		},
	}
}

func (t *PantryGet) OutputSchema() *jsonschema.Schema {
	minQty := 0.0
	minDays := 0.0
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"pantry": {
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"ingredients": {
						Type: "array",
						Items: &jsonschema.Schema{
							Type: "object",
							Properties: map[string]*jsonschema.Schema{
								"name":      {Type: "string"},
								"qty":       {Type: "number", Minimum: &minQty},
								"unit":      {Type: "string"},
								"days_left": {Type: "integer", Minimum: &minDays},
							},
							Required: []string{"name", "qty", "unit", "days_left"},
						},
					},
				},
				Required: []string{"ingredients"},
			},
		},
		Required: []string{"pantry"},
	}
}

func (t *PantryGet) Run(ctx context.Context, input map[string]any) (map[string]any, error) {
	current := 0
	if v, ok := input["current_day"].(float64); ok {
		current = int(v)
	}

	pan, err := t.load(ctx)
	if err != nil {
		return nil, err
	}

	type outIng struct {
		Name string  `json:"name"`
		Qty  float64 `json:"qty"`
		Unit string  `json:"unit"`
		Days int     `json:"days_left"`
	}
	out := struct {
		Pantry struct {
			Ingredients []outIng `json:"ingredients"`
		} `json:"pantry"`
	}{}

	// Initialize ingredients slice to prevent nil when empty
	out.Pantry.Ingredients = make([]outIng, 0)

	for _, it := range pan.Ingredients {
		out.Pantry.Ingredients = append(out.Pantry.Ingredients, outIng{
			Name: it.Name, Qty: it.Qty, Unit: it.Unit, Days: getDaysLeft(it, current),
		})
	}

	// marshal -> map[string]any to keep outputs uniform
	b, _ := json.Marshal(out)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, nil
}

func (t *PantryGet) load(ctx context.Context) (Pantry, error) {
	b, err := t.state.Load(ctx)
	if err != nil {
		return Pantry{}, fmt.Errorf("read pantry: %w", err)
	}
	var p Pantry
	return p, json.Unmarshal(b, &p)
}

func remainingFreshness(ing Ingredient, currentDay int) int {
	if ing.PerishableDays == 0 {
		return 9999
	}
	return ing.PerishableDays - (currentDay - ing.AddedDay)
}

func getDaysLeft(ing Ingredient, currentDay int) int {
	// If days_left is directly specified in the JSON, use that
	if ing.DaysLeft > 0 {
		return ing.DaysLeft
	}
	// Otherwise, fall back to the calculated method
	return remainingFreshness(ing, currentDay)
}
