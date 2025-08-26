package ollama

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"pantryagent"
	"pantryagent/tools"

	"github.com/modelcontextprotocol/go-sdk/jsonschema"
	"go.opentelemetry.io/otel/sdk/trace"
)

// Mock LLM Client
type mockLLMClient struct {
	responses []Response
	callCount int
	shouldErr bool
}

func (m *mockLLMClient) Invoke(ctx context.Context, prompt Prompt) (Response, error) {
	if m.shouldErr {
		return Response{}, errors.New("mock LLM error")
	}

	if m.callCount >= len(m.responses) {
		return Response{Content: "No more responses configured"}, nil
	}

	response := m.responses[m.callCount]
	m.callCount++
	return response, nil
}

// Mock Tool Provider
type mockToolProvider struct {
	tools []tools.Tool
}

func (m *mockToolProvider) GetTools() []tools.Tool {
	return m.tools
}

func (m *mockToolProvider) GetTool(name string) (tools.Tool, error) {
	for _, tool := range m.tools {
		if tool.Name() == name {
			return tool, nil
		}
	}
	return nil, fmt.Errorf("tool not found: %s", name)
}

// Mock Tool
type mockTool struct {
	name      string
	shouldErr bool
	callCount int
	result    map[string]any
}

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Title() string {
	return m.name + " Tool"
}

func (m *mockTool) Description() string {
	return "Mock tool for testing"
}

func (m *mockTool) InputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"current_day": {Type: "integer"},
			"meal_types":  {Type: "array"},
		},
	}
}

func (m *mockTool) OutputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"result": {Type: "string"},
		},
	}
}

func (m *mockTool) Run(ctx context.Context, input map[string]any) (output map[string]any, err error) {
	m.callCount++

	if m.shouldErr {
		return nil, fmt.Errorf("mock tool error: %s", m.name)
	}

	if m.result != nil {
		return m.result, nil
	}

	return map[string]any{
		"result": fmt.Sprintf("Mock result from %s", m.name),
		"input":  input,
	}, nil
}

func TestNewCoordinator(t *testing.T) {
	llm := &mockLLMClient{}
	tp := &mockToolProvider{}
	logger := pantryagent.NewNoOpCoordinationLogger()
	tracerProvider := trace.NewTracerProvider()

	coord := NewCoordinator(llm, tp, 5, logger, tracerProvider)

	if coord.llm != llm {
		t.Error("Expected LLM client to be set")
	}
	if coord.toolProvider != tp {
		t.Error("Expected tool provider to be set")
	}
	if coord.maxIterations != 5 {
		t.Error("Expected max iterations to be 5")
	}
	if coord.logger != logger {
		t.Error("Expected logger to be set")
	}
}

func TestCoordinator_Run(t *testing.T) {
	tests := []struct {
		name           string
		llmResponses   []Response
		llmShouldErr   bool
		tools          []tools.Tool
		toolShouldErr  bool
		maxIterations  int
		expectedResult string
		expectError    bool
	}{
		{
			name: "successful meal planning",
			llmResponses: []Response{
				{
					ToolCalls: []ToolCall{
						{Name: "pantry_get", Args: map[string]any{"current_day": 0}},
						{Name: "recipe_get", Args: map[string]any{}},
					},
				},
				{
					Content: `{"summary": "Simple meal plan", "days_planned": [{"day": 1, "meals": [{"id": "recipe1", "name": "Chicken Dinner", "servings": 4}]}]}`,
				},
			},
			tools: []tools.Tool{
				&mockTool{
					name: "pantry_get",
					result: map[string]any{
						"ingredients": []map[string]any{
							{"name": "chicken", "quantity": 2.0, "unit": "lbs", "days_left": 2},
						},
					},
				},
				&mockTool{
					name: "recipe_get",
					result: map[string]any{
						"recipes": []map[string]any{
							{"id": "recipe1", "name": "Chicken Dinner", "servings": 4},
						},
					},
				},
			},
			expectedResult: `{"summary": "Simple meal plan", "days_planned": [{"day": 1, "meals": [{"id": "recipe1", "name": "Chicken Dinner", "servings": 4}]}]}`,
			expectError:    false,
		},
		{
			name:         "LLM error",
			llmShouldErr: true,
			tools:        []tools.Tool{},
			expectError:  true,
		},
		{
			name: "tool error",
			llmResponses: []Response{
				{
					ToolCalls: []ToolCall{
						{Name: "pantry_get", Args: map[string]any{"current_day": 0}},
					},
				},
			},
			tools: []tools.Tool{
				&mockTool{name: "pantry_get", shouldErr: true},
			},
			expectError: true,
		},
		{
			name: "tool not found",
			llmResponses: []Response{
				{
					ToolCalls: []ToolCall{
						{Name: "nonexistent_tool", Args: map[string]any{}},
					},
				},
			},
			tools:       []tools.Tool{},
			expectError: true,
		},
		{
			name: "empty response error",
			llmResponses: []Response{
				{}, // Empty response
			},
			tools:       []tools.Tool{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := &mockLLMClient{
				responses: tt.llmResponses,
				shouldErr: tt.llmShouldErr,
			}

			tp := &mockToolProvider{tools: tt.tools}

			logger := pantryagent.NewNoOpCoordinationLogger()

			maxIter := tt.maxIterations
			if maxIter == 0 {
				maxIter = 5
			}

			coord := NewCoordinator(llm, tp, maxIter, logger, trace.NewTracerProvider())

			result, err := coord.Run(context.Background(), "Plan meals for 1 day")

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !tt.expectError && result != tt.expectedResult {
				t.Errorf("Expected result %q, got %q", tt.expectedResult, result)
			}
		})
	}
}

func TestCoordinator_Run_WithToolResults(t *testing.T) {
	// Test scenario where tools have already been called
	pantryTool := &mockTool{name: "pantry_get"}
	recipeTool := &mockTool{name: "recipe_get"}
	tp := &mockToolProvider{tools: []tools.Tool{pantryTool, recipeTool}}

	llm := &mockLLMClient{
		responses: []Response{
			// First call: call tools
			{
				ToolCalls: []ToolCall{
					{Name: "pantry_get", Args: map[string]any{"current_day": 0}},
					{Name: "recipe_get", Args: map[string]any{}},
				},
			},
			// Second call: return final JSON
			{
				Content: `{"summary": "Quick plan", "days_planned": [{"day": 1, "meals": [{"id": "recipe1", "name": "Quick Meal", "servings": 2}]}]}`,
			},
		},
	}

	logger := pantryagent.NewNoOpCoordinationLogger()
	tracerProvider := trace.NewTracerProvider()
	coord := NewCoordinator(llm, tp, 5, logger, tracerProvider)

	result, err := coord.Run(context.Background(), "Plan meals")

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expected := `{"summary": "Quick plan", "days_planned": [{"day": 1, "meals": [{"id": "recipe1", "name": "Quick Meal", "servings": 2}]}]}`
	if result != expected {
		t.Errorf("Expected result %q, got %q", expected, result)
	}

	// Verify tools were called
	if pantryTool.callCount != 1 {
		t.Errorf("Expected pantry_get to be called 1 time, was called %d times", pantryTool.callCount)
	}
	if recipeTool.callCount != 1 {
		t.Errorf("Expected recipe_get to be called 1 time, was called %d times", recipeTool.callCount)
	}
}

func TestDedupeToolCalls(t *testing.T) {
	tests := []struct {
		name     string
		input    []ToolCall
		expected int
	}{
		{
			name: "no duplicates",
			input: []ToolCall{
				{Name: "pantry_get", Args: map[string]any{"current_day": 0}},
				{Name: "recipe_get", Args: map[string]any{"meal_types": []string{"dinner"}}},
			},
			expected: 2,
		},
		{
			name: "exact duplicates",
			input: []ToolCall{
				{Name: "pantry_get", Args: map[string]any{"current_day": 0}},
				{Name: "pantry_get", Args: map[string]any{"current_day": 0}},
			},
			expected: 1,
		},
		{
			name: "same tool different args",
			input: []ToolCall{
				{Name: "recipe_get", Args: map[string]any{"meal_types": []string{"dinner"}}},
				{Name: "recipe_get", Args: map[string]any{"meal_types": []string{"lunch"}}},
			},
			expected: 2,
		},
		{
			name: "mixed scenario",
			input: []ToolCall{
				{Name: "pantry_get", Args: map[string]any{"current_day": 0}},
				{Name: "recipe_get", Args: map[string]any{"meal_types": []string{"dinner"}}},
				{Name: "pantry_get", Args: map[string]any{"current_day": 0}},                // Duplicate
				{Name: "recipe_get", Args: map[string]any{"meal_types": []string{"lunch"}}}, // Different args
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dedupeToolCalls(tt.input)

			if len(result) != tt.expected {
				t.Errorf("Expected %d calls after dedup, got %d", tt.expected, len(result))
			}
		})
	}
}
