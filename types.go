package pantryagent

import (
	"context"
	"net/http"
	"pantryagent/tools"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type SlackClient interface {
	PostMessage(ctx context.Context, channel string, message string) error
}

type ToolProvider interface {
	GetTools() []tools.Tool
	GetTool(name string) (tools.Tool, error)
}

type Coordinator interface {
	Run(ctx context.Context, task string) (string, error)
}

// MealPlan represents the final meal plan structure expected from the LLM
type MealPlan struct {
	Summary     string    `json:"summary"`
	DaysPlanned []DayPlan `json:"days_planned"`
}

// DayPlan represents a single day's meal plan
type DayPlan struct {
	Day   int    `json:"day"`
	Meals []Meal `json:"meals"`
}

// Meal represents a single meal in the plan
type Meal struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Servings int    `json:"servings"`
}

// IsValid checks if the MealPlan meets basic validation requirements
func (mp *MealPlan) IsValid() bool {
	// Must have at least one day planned
	if len(mp.DaysPlanned) == 0 {
		return false
	}

	// Each day must have at least one meal
	for _, day := range mp.DaysPlanned {
		if len(day.Meals) == 0 {
			return false
		}

		// Each meal must have valid fields
		for _, meal := range day.Meals {
			if meal.ID == "" || meal.Name == "" || meal.Servings <= 0 {
				return false
			}
		}
	}

	// Summary should not be empty
	if mp.Summary == "" {
		return false
	}

	return true
}
