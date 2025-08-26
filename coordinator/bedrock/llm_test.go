package bedrock

import (
	"context"
	"pantryagent/tools"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBedrockClient implements bedrockRuntimeClient for testing
type mockBedrockClient struct {
	response *bedrockruntime.ConverseOutput
	err      error
}

func (m *mockBedrockClient) Converse(ctx context.Context, input *bedrockruntime.ConverseInput, opts ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	return m.response, m.err
}

func TestNewLLMClient(t *testing.T) {
	tests := []struct {
		name     string
		input    LLMOptions
		expected LLMOptions
	}{
		{
			name:  "empty options uses defaults",
			input: LLMOptions{},
			expected: LLMOptions{
				ModelID:     defaultModelID,
				MaxTokens:   defaultMaxTokens,
				Temperature: defaultTemperature,
				TopP:        defaultTopP,
			},
		},
		{
			name: "custom options preserved",
			input: LLMOptions{
				ModelID:     "custom-model",
				MaxTokens:   2048,
				Temperature: 0.5,
				TopP:        0.8,
			},
			expected: LLMOptions{
				ModelID:     "custom-model",
				MaxTokens:   2048,
				Temperature: 0.5,
				TopP:        0.8,
			},
		},
		{
			name: "partial options with defaults",
			input: LLMOptions{
				ModelID:   "custom-model",
				MaxTokens: 2048,
			},
			expected: LLMOptions{
				ModelID:     "custom-model",
				MaxTokens:   2048,
				Temperature: defaultTemperature,
				TopP:        defaultTopP,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockBedrockClient{}
			client := NewLLMClient(mockClient, tt.input)

			assert.Equal(t, tt.expected, client.opts)
			assert.Equal(t, mockClient, client.brc)
		})
	}
}

func TestLLMClient_Invoke(t *testing.T) {
	tests := []struct {
		name          string
		prompt        Prompt
		mockResponse  *bedrockruntime.ConverseOutput
		mockError     error
		expectedResp  Response
		expectedError string
	}{
		{
			name: "successful text response",
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: MessageParts{{Type: "text", Text: "Hello"}}},
				},
			},
			mockResponse: &bedrockruntime.ConverseOutput{
				StopReason: "end_turn",
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{Value: `{"meals": []}`},
						},
					},
				},
				Usage: &types.TokenUsage{
					InputTokens:  aws.Int32(10),
					OutputTokens: aws.Int32(20),
				},
				Metrics: &types.ConverseMetrics{
					LatencyMs: aws.Int64(100),
				},
			},
			expectedResp: Response{Content: `{"meals": []}`},
		},
		{
			name: "tool use response",
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: MessageParts{{Type: "text", Text: "Get pantry data"}}},
				},
			},
			mockResponse: &bedrockruntime.ConverseOutput{
				StopReason: "tool_use",
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Content: []types.ContentBlock{
							&types.ContentBlockMemberToolUse{
								Value: types.ToolUseBlock{
									ToolUseId: aws.String("test-id"),
									Name:      aws.String("pantry_get"),
									Input:     document.NewLazyDocument(map[string]any{}),
								},
							},
						},
					},
				},
				Usage: &types.TokenUsage{
					InputTokens:  aws.Int32(10),
					OutputTokens: aws.Int32(20),
				},
				Metrics: &types.ConverseMetrics{
					LatencyMs: aws.Int64(100),
				},
			},
			expectedResp: Response{
				ToolCalls: []tools.Call{
					{Name: "pantry_get", Input: map[string]any{}, ToolUseID: "test-id"},
				},
			},
		},
		{
			name: "max tokens error",
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: MessageParts{{Type: "text", Text: "Hello"}}},
				},
			},
			mockResponse: &bedrockruntime.ConverseOutput{
				StopReason: "max_tokens",
				Usage: &types.TokenUsage{
					InputTokens:  aws.Int32(10),
					OutputTokens: aws.Int32(20),
				},
				Metrics: &types.ConverseMetrics{
					LatencyMs: aws.Int64(100),
				},
			},
			expectedError: "model hit MaxTokens limit",
		},
		{
			name: "safety filter error",
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: MessageParts{{Type: "text", Text: "Hello"}}},
				},
			},
			mockResponse: &bedrockruntime.ConverseOutput{
				StopReason: "content_filtered",
				Usage: &types.TokenUsage{
					InputTokens:  aws.Int32(10),
					OutputTokens: aws.Int32(20),
				},
				Metrics: &types.ConverseMetrics{
					LatencyMs: aws.Int64(100),
				},
			},
			expectedError: "model response blocked by Bedrock safety filters",
		},
		{
			name: "invalid final JSON",
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: MessageParts{{Type: "text", Text: "Hello"}}},
				},
			},
			mockResponse: &bedrockruntime.ConverseOutput{
				StopReason: "end_turn",
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{Value: "invalid json"},
						},
					},
				},
				Usage: &types.TokenUsage{
					InputTokens:  aws.Int32(10),
					OutputTokens: aws.Int32(20),
				},
				Metrics: &types.ConverseMetrics{
					LatencyMs: aws.Int64(100),
				},
			},
			expectedError: "final output not valid JSON",
		},
		{
			name: "bedrock API error",
			prompt: Prompt{
				Messages: []Message{
					{Role: "user", Content: MessageParts{{Type: "text", Text: "Hello"}}},
				},
			},
			mockError:     assert.AnError,
			expectedError: "assert.AnError general error for testing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mockBedrockClient{
				response: tt.mockResponse,
				err:      tt.mockError,
			}

			llmClient := NewLLMClient(mockClient, LLMOptions{})
			resp, err := llmClient.Invoke(context.Background(), tt.prompt)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedResp, resp)
		})
	}
}

func TestTextFromOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   *bedrockruntime.ConverseOutput
		expected string
	}{
		{
			name:     "nil output",
			output:   nil,
			expected: "",
		},
		{
			name: "single text block",
			output: &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{Value: "Hello world"},
						},
					},
				},
			},
			expected: "Hello world",
		},
		{
			name: "multiple text blocks",
			output: &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{Value: "Hello"},
							&types.ContentBlockMemberText{Value: "world"},
						},
					},
				},
			},
			expected: "Hello\nworld",
		},
		{
			name: "prefer JSON object",
			output: &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{Value: "Some text"},
							&types.ContentBlockMemberText{Value: `{"key": "value"}`},
						},
					},
				},
			},
			expected: `{"key": "value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := textFromOutput(tt.output)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeInput(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{
			name:     "whole number float to int",
			input:    2.0,
			expected: 2,
		},
		{
			name:     "decimal float unchanged",
			input:    2.5,
			expected: 2.5,
		},
		{
			name:     "string unchanged",
			input:    "hello",
			expected: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeInput(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToolCallsFromOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   *bedrockruntime.ConverseOutput
		expected []tools.Call
	}{
		{
			name: "single tool call",
			output: &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Content: []types.ContentBlock{
							&types.ContentBlockMemberToolUse{
								Value: types.ToolUseBlock{
									ToolUseId: aws.String("test-id"),
									Name:      aws.String("pantry_get"),
									Input:     document.NewLazyDocument(map[string]any{}),
								},
							},
						},
					},
				},
			},
			expected: []tools.Call{
				{Name: "pantry_get", Input: map[string]any{}, ToolUseID: "test-id"},
			},
		},
		{
			name: "multiple tool calls",
			output: &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Content: []types.ContentBlock{
							&types.ContentBlockMemberToolUse{
								Value: types.ToolUseBlock{
									ToolUseId: aws.String("id1"),
									Name:      aws.String("tool1"),
									Input:     document.NewLazyDocument(map[string]any{}),
								},
							},
							&types.ContentBlockMemberToolUse{
								Value: types.ToolUseBlock{
									ToolUseId: aws.String("id2"),
									Name:      aws.String("tool2"),
									Input:     document.NewLazyDocument(map[string]any{}),
								},
							},
						},
					},
				},
			},
			expected: []tools.Call{
				{Name: "tool1", Input: map[string]any{}, ToolUseID: "id1"},
				{Name: "tool2", Input: map[string]any{}, ToolUseID: "id2"},
			},
		},
		{
			name: "no tool calls",
			output: &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Content: []types.ContentBlock{
							&types.ContentBlockMemberText{Value: "Just text"},
						},
					},
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := toolCallsFromOutput(tt.output)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildToolSpec(t *testing.T) {
	tests := []struct {
		name        string
		tool        Tool
		expectError bool
	}{
		{
			name: "basic tool spec",
			tool: Tool{
				Name:        "pantry_get",
				Description: "Get pantry data",
				InputSchema: &jsonschema.Schema{Type: "object"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildToolSpec(tt.tool)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.tool.Name, *result.Name)
			assert.Equal(t, tt.tool.Description, *result.Description)
			assert.NotNil(t, result.InputSchema)
		})
	}
}
