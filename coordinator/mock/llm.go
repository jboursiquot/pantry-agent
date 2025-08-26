package mock

import (
	"context"
	"encoding/json"
	"log/slog"
)

type LLMClient struct{}

func NewLLMClient(_ Prompt) *LLMClient {
	return &LLMClient{}
}

// Invoke is a mock implementation that simulates an LLM response based on the presence of tool results in the prompt. It is, of course, deterministic and only serves as a learning aid to see how the coordinator handles different phases of tool use and response generation. Real LLMs may not be so kind :)
func (m *LLMClient) Invoke(ctx context.Context, prompt Prompt) (Response, error) {
	slog.Info("LLM_CLIENT: Invoked", "messages_len", len(prompt.Messages))

	// Phase 1: no results yet -> plan to fetch pantry + recipes
	if !prompt.HasToolResultInContent("pantry_get") && !prompt.HasToolResultInContent("recipe_get") {
		plan := map[string]any{
			"tool_calls": []map[string]any{
				{"name": "pantry_get", "input": map[string]any{"current_day": 0}},
				{"name": "recipe_get", "input": map[string]any{"meal_types": []string{"dinner"}}},
			},
		}
		b, err := json.Marshal(plan)
		if err != nil {
			slog.Error("Failed to marshal plan", "error", err)
			return Response{Content: ""}, nil
		}

		slog.Info("LLM_CLIENT: Returning plan for pantry_get and recipe_get")

		return Response{Content: string(b)}, nil
	}

	// Phase 2: all tool results present -> return final structured plan
	if prompt.HasToolResultInContent("pantry_get") && prompt.HasToolResultInContent("recipe_get") {
		final := map[string]any{
			"summary": "Planned 1 dinner prioritizing items with low days_left.",
			"days_planned": []map[string]any{
				{
					"day": 1,
					"meals": []map[string]any{
						{"id": "dinner_bean_chili", "name": "Bean Chili", "servings": 2},
					},
				},
			},
		}
		b, err := json.Marshal(final)
		if err != nil {
			slog.Error("Failed to marshal final response", "error", err)
			return Response{Content: ""}, nil
		}

		slog.Info("LLM_CLIENT: Returning final meal plan")

		return Response{Content: string(b)}, nil
	}

	// Phase 3: fallback plan
	plan := map[string]any{
		"tool_calls": []map[string]any{
			{"name": "pantry_get", "input": map[string]any{"current_day": 0}},
			{"name": "recipe_get", "input": map[string]any{"meal_types": []string{"dinner"}}},
		},
	}
	b, err := json.Marshal(plan)
	if err != nil {
		slog.Error("Failed to marshal fallback plan", "error", err)
		return Response{Content: ""}, nil
	}

	slog.Info("LLM_CLIENT: Returning fallback plan for pantry_get and recipe_get")

	return Response{Content: string(b)}, nil
}
