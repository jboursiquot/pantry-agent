package ollama

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// mockHTTPClient implements the HTTPClient interface for testing
type mockHTTPClient struct {
	response *http.Response
	err      error
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.response, m.err
}

// createMockResponse creates a mock HTTP response
func createMockResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		opts    ClientOpts
		want    *Client
		wantErr bool
	}{
		{
			name: "valid client creation",
			opts: ClientOpts{
				BaseEndpoint: "http://localhost:11434",
				ModelID:      "llama3.2",
				Prompt: Prompt{
					Messages: []Message{
						{Role: "system", Content: "You are a helpful assistant"},
					},
				},
				HTTPClient: &mockHTTPClient{},
			},
			want: &Client{
				model:        "llama3.2",
				systemPrompt: "You are a helpful assistant",
				endpoint:     "http://localhost:11434/api/chat",
				options: options{
					Temperature:   0.2,
					TopP:          0.9,
					RepeatPenalty: 1.05,
					NumCtx:        16384,
				},
			},
			wantErr: false,
		},
		{
			name: "empty system prompt",
			opts: ClientOpts{
				BaseEndpoint: "http://localhost:11434",
				ModelID:      "llama3.2",
				Prompt:       Prompt{Messages: []Message{}},
				HTTPClient:   &mockHTTPClient{},
			},
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewClient(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.model != tt.want.model {
				t.Errorf("NewClient() model = %v, want %v", got.model, tt.want.model)
			}
			if got.systemPrompt != tt.want.systemPrompt {
				t.Errorf("NewClient() systemPrompt = %v, want %v", got.systemPrompt, tt.want.systemPrompt)
			}
			if got.endpoint != tt.want.endpoint {
				t.Errorf("NewClient() endpoint = %v, want %v", got.endpoint, tt.want.endpoint)
			}
		})
	}
}

func TestClient_Invoke(t *testing.T) {
	tests := []struct {
		name           string
		mockResponse   *http.Response
		mockError      error
		prompt         Prompt
		expectedResult Response
		wantErr        bool
		errContains    string
	}{
		{
			name: "successful response with content",
			mockResponse: createMockResponse(200, `{
				"message": {
					"role": "assistant",
					"content": "Here's your meal plan: ..."
				}
			}`),
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: "Plan meals for 3 days"},
				},
			},
			expectedResult: Response{
				Content: "Here's your meal plan: ...",
			},
			wantErr: false,
		},
		{
			name: "successful response with tool calls",
			mockResponse: createMockResponse(200, `{
				"message": {
					"role": "assistant",
					"content": "I need to check your pantry first.",
					"tool_calls": [
						{
							"function": {
								"name": "pantry_get",
								"arguments": {"current_day": 0}
							}
						}
					]
				}
			}`),
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: "Plan meals for 3 days"},
				},
				Tools: []Tool{
					{
						Type: "function",
						Function: ToolSchema{
							Name:        "pantry_get",
							Description: "Get pantry contents",
							Parameters: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"current_day": map[string]any{"type": "integer"},
								},
							},
						},
					},
				},
			},
			expectedResult: Response{
				Content: "I need to check your pantry first.",
				ToolCalls: []ToolCall{
					{
						Name: "pantry_get",
						Args: map[string]any{"current_day": float64(0)},
					},
				},
			},
			wantErr: false,
		},
		{
			name:         "HTTP error",
			mockResponse: createMockResponse(500, `{"error": "Internal server error"}`),
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: "Plan meals"},
				},
			},
			expectedResult: Response{},
			wantErr:        true,
			errContains:    "LLM_CLIENT:",
		},
		{
			name:           "network error",
			mockError:      io.EOF,
			prompt:         Prompt{Messages: []Message{{Role: "user", Content: "Plan meals"}}},
			expectedResult: Response{},
			wantErr:        true,
		},
		{
			name: "malformed JSON response",
			mockResponse: createMockResponse(200, `{
				"message": {
					"role": "assistant",
					"content": "Invalid JSON response"
				}
			`), // Missing closing brace
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: "Plan meals"},
				},
			},
			expectedResult: Response{
				Content: `{
				"message": {
					"role": "assistant",
					"content": "Invalid JSON response"
				}
			`,
			},
			wantErr: false, // Should handle malformed JSON gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				model:        "llama3.2",
				systemPrompt: "You are a helpful assistant",
				httpClient:   &mockHTTPClient{response: tt.mockResponse, err: tt.mockError},
				endpoint:     "http://localhost:11434/api/chat",
				options: options{
					Temperature: 0.2,
					TopP:        0.9,
				},
			}

			result, err := client.Invoke(context.Background(), tt.prompt)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Invoke() expected error but got none")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Invoke() error = %v, expected to contain %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Invoke() unexpected error = %v", err)
				return
			}

			if result.Content != tt.expectedResult.Content {
				t.Errorf("Invoke() content = %v, want %v", result.Content, tt.expectedResult.Content)
			}

			if len(result.ToolCalls) != len(tt.expectedResult.ToolCalls) {
				t.Errorf("Invoke() tool calls count = %v, want %v", len(result.ToolCalls), len(tt.expectedResult.ToolCalls))
				return
			}

			for i, call := range result.ToolCalls {
				expected := tt.expectedResult.ToolCalls[i]
				if call.Name != expected.Name {
					t.Errorf("Invoke() tool call %d name = %v, want %v", i, call.Name, expected.Name)
				}
				// Compare args using JSON marshaling for deep comparison
				callArgs, _ := json.Marshal(call.Args)
				expectedArgs, _ := json.Marshal(expected.Args)
				if string(callArgs) != string(expectedArgs) {
					t.Errorf("Invoke() tool call %d args = %v, want %v", i, call.Args, expected.Args)
				}
			}
		})
	}
}

func TestClient_buildRequest(t *testing.T) {
	tests := []struct {
		name         string
		systemPrompt string
		prompt       Prompt
		expected     []Message
	}{
		{
			name:         "basic user message with system prompt",
			systemPrompt: "You are a helpful assistant",
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: "Hello"},
				},
			},
			expected: []Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{Role: "user", Content: "Hello"},
			},
		},
		{
			name:         "conversation with tool message",
			systemPrompt: "You are a meal planner",
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: "Plan meals"},
					{Role: "assistant", Content: "I'll check your pantry"},
					{Role: "tool", Name: "pantry_get", Content: `{"items": []}`},
				},
			},
			expected: []Message{
				{Role: "system", Content: "You are a meal planner"},
				{Role: "user", Content: "Plan meals"},
				{Role: "assistant", Content: "I'll check your pantry"},
				{Role: "tool", Name: "pantry_get", Content: `{"items": []}`},
			},
		},
		{
			name:         "skip system messages from prompt",
			systemPrompt: "You are a helpful assistant",
			prompt: Prompt{
				Messages: []Message{
					{Role: "system", Content: "This should be ignored"},
					{Role: "user", Content: "Hello"},
				},
			},
			expected: []Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{Role: "user", Content: "Hello"},
			},
		},
		{
			name:         "handle unknown role",
			systemPrompt: "You are a helpful assistant",
			prompt: Prompt{
				Messages: []Message{
					{Role: "unknown", Content: "This should be treated as user"},
				},
			},
			expected: []Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{Role: "user", Content: "This should be treated as user"},
			},
		},
		{
			name:         "tool message without name is skipped",
			systemPrompt: "You are a helpful assistant",
			prompt: Prompt{
				Messages: []Message{
					{Role: "tool", Content: "No name, should be skipped"},
					{Role: "user", Content: "Hello"},
				},
			},
			expected: []Message{
				{Role: "system", Content: "You are a helpful assistant"},
				{Role: "user", Content: "Hello"},
			},
		},
		{
			name:         "empty system prompt",
			systemPrompt: "",
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: "Hello"},
				},
			},
			expected: []Message{
				{Role: "user", Content: "Hello"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				systemPrompt: tt.systemPrompt,
			}

			result, err := client.buildRequest(tt.prompt)
			if err != nil {
				t.Errorf("buildRequest() unexpected error = %v", err)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("buildRequest() message count = %v, want %v", len(result), len(tt.expected))
				return
			}

			for i, msg := range result {
				expected := tt.expected[i]
				if msg.Role != expected.Role {
					t.Errorf("buildRequest() message %d role = %v, want %v", i, msg.Role, expected.Role)
				}
				if msg.Content != expected.Content {
					t.Errorf("buildRequest() message %d content = %v, want %v", i, msg.Content, expected.Content)
				}
				if msg.Name != expected.Name {
					t.Errorf("buildRequest() message %d name = %v, want %v", i, msg.Name, expected.Name)
				}
			}
		})
	}
}
