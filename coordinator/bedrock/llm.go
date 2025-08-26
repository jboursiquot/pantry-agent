package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"pantryagent"
	"strings"

	"pantryagent/tools"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

const (
	// defaultModelID is the default model ID for Bedrock Claude.
	// It's an inference profile ID or ARN, not the foundation model's ID.
	// See https://docs.aws.amazon.com/bedrock/latest/userguide/inference-profiles.html.
	defaultModelID = "us.anthropic.claude-3-7-sonnet-20250219-v1:0"

	// Controls the maximum number of tokens the model can generate in one response.
	// 1k is a good balance for cost + safety. Raise it (e.g. 2048–4096) when expecting longer responses.
	defaultMaxTokens = 1024

	// Controls the randomness of the model's output. Low temperature keeps outputs more deterministic and consistent, which is better for tool use, JSON, and structured outputs.
	defaultTemperature = 0.2

	// Controls the diversity of the model's output. Low top_p keeps outputs more focused and less random, which is better for tool use, JSON, and structured outputs.
	defaultTopP = 0.9
)

type bedrockRuntimeClient interface {
	Converse(context.Context, *bedrockruntime.ConverseInput, ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
}

type LLMOptions struct {
	ModelID     string
	MaxTokens   int32
	Temperature float32
	TopP        float32
}

type LLMClient struct {
	brc  bedrockRuntimeClient
	opts LLMOptions
}

func NewLLMClient(brc bedrockRuntimeClient, opts LLMOptions) *LLMClient {
	if opts.ModelID == "" {
		opts.ModelID = defaultModelID
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = defaultMaxTokens
	}
	if opts.Temperature == 0 {
		opts.Temperature = defaultTemperature
	}
	if opts.TopP == 0 {
		opts.TopP = defaultTopP
	}
	return &LLMClient{
		brc:  brc,
		opts: opts,
	}
}

func (c *LLMClient) Invoke(ctx context.Context, prompt Prompt) (Response, error) {
	slog.Info("LLM_CLIENT: Invoked", "messages_len", len(prompt.Messages))

	// Build system block
	var sys []types.SystemContentBlock
	for _, m := range prompt.Messages {
		if m.Role == "system" {
			sys = append(sys, &types.SystemContentBlockMemberText{Value: m.Content.Join()})
			slog.Info("LLM_CLIENT: Added system content", "text", len(m.Content.Join()))
		}
	}

	// Build messages
	var msgs []types.Message
	for _, m := range prompt.Messages {
		if m.Role == "system" {
			continue // already handled above
		}
		msg := types.Message{Role: types.ConversationRole(m.Role)}

		for _, part := range m.Content {
			switch part.Type {
			case "text":
				msg.Content = append(msg.Content, &types.ContentBlockMemberText{Value: part.Text})

				slog.Info("LLM_CLIENT: Added text content", "text_len", len(part.Text))

			case "tool_use":

				// Build a ToolUse block for assistant messages
				// TODO: inefficient, address
				input := make(map[string]any)
				inputBytes, _ := json.Marshal(part.Data)
				if err := json.Unmarshal(inputBytes, &input); err != nil {
					slog.Error("LLM_CLIENT: Failed to create fresh input data", "error", err)
					input = make(map[string]any)
					for k, v := range part.Data {
						input[k] = v
					}
				}

				tub := types.ToolUseBlock{
					ToolUseId: aws.String(part.ToolUseID),
					Name:      aws.String(part.ToolName),
					Input:     document.NewLazyDocument(input),
				}
				msg.Content = append(msg.Content, &types.ContentBlockMemberToolUse{Value: tub})

				slog.Info("LLM_CLIENT: Added tool use content",
					"tool_name", part.ToolName,
					"tool_use_id", part.ToolUseID,
					"input", part.Data)

			case "tool_result":
				// Build a ToolResult block tied to the original toolUseId
				if part.Data == nil {
					panic("tool result data cannot be nil")
				}

				result := make(map[string]any)
				resultBytes, _ := json.Marshal(part.Data)
				if err := json.Unmarshal(resultBytes, &result); err != nil {
					slog.Error("LLM_CLIENT: Failed to create fresh result data", "error", err)
					result = make(map[string]any)
					for k, v := range part.Data {
						result[k] = v
					}
				}

				tr := types.ToolResultBlock{
					ToolUseId: aws.String(part.ToolUseID),
					Status:    types.ToolResultStatusSuccess,
					Content: []types.ToolResultContentBlock{
						&types.ToolResultContentBlockMemberJson{
							Value: document.NewLazyDocument(result),
						},
					},
				}
				msg.Content = append(msg.Content, &types.ContentBlockMemberToolResult{Value: tr})

				slog.Info("LLM_CLIENT: Added tool result content", "tool_use_id", part.ToolUseID, "result_len", len(result))
			}
		}

		msgs = append(msgs, msg)
	}

	// Build tools
	var tools []types.Tool
	for _, t := range prompt.Tools {
		spec, err := buildToolSpec(t)
		if err != nil {
			slog.Error("LLM_CLIENT: Failed to build tool spec", "error", err)
			continue
		}
		tools = append(tools, &types.ToolMemberToolSpec{Value: spec})
		slog.Info("LLM_CLIENT: Registered tool", "name", t.Name, "description", t.Description, "input_schema", t.InputSchema)
	}

	// Invoke the Bedrock Converse API
	in := &bedrockruntime.ConverseInput{
		ModelId:  &c.opts.ModelID,
		System:   sys,
		Messages: msgs,
		InferenceConfig: &types.InferenceConfiguration{
			MaxTokens:   aws.Int32(c.opts.MaxTokens),
			Temperature: aws.Float32(c.opts.Temperature),
			TopP:        aws.Float32(c.opts.TopP),
		},
		ToolConfig: &types.ToolConfiguration{Tools: tools, ToolChoice: &types.ToolChoiceMemberAuto{}},
	}
	out, err := c.brc.Converse(ctx, in)
	if err != nil {
		inPayload, _ := json.Marshal(in)
		slog.Error("LLM_CLIENT: Bedrock Claude invoke failed", "error", err, "input", string(inPayload))
		return Response{}, err
	}

	slog.Info("LLM_CLIENT: Bedrock Claude invoke succeeded",
		"stop_reason", out.StopReason,
		"latency_ms", aws.ToInt64(out.Metrics.LatencyMs),
		"input_tokens", aws.ToInt32(out.Usage.InputTokens),
		"output_tokens", aws.ToInt32(out.Usage.OutputTokens),
	)

	switch out.StopReason {
	case "tool_use":
		calls, err := toolCallsFromOutput(out)
		if err != nil {
			return Response{}, fmt.Errorf("failed to parse tool calls: %w", err)
		}
		slog.Info("LLM_CLIENT: Extracted tool calls", "calls_len", len(calls))
		return Response{ToolCalls: calls}, nil

	case "end_turn", "stop_sequence":
		text, err := textFromOutput(out)
		if err != nil {
			return Response{}, fmt.Errorf("failed to extract final text: %w", err)
		}

		// Validate the final output against the MealPlan schema
		if err := json.Unmarshal([]byte(text), &pantryagent.MealPlan{}); err != nil {
			return Response{}, fmt.Errorf("final output not valid JSON: %w", err)
		}

		slog.Info("LLM_CLIENT: Extracted final text", "text_len", len(text))
		return Response{Content: text}, nil

	case "max_tokens":
		slog.Warn("LLM_CLIENT: Model hit MaxTokens limit; consider increasing MaxTokens or chunking")
		return Response{}, fmt.Errorf("model hit MaxTokens limit; consider increasing MaxTokens or chunking")

	case "safety", "content_filtered":
		slog.Warn("LLM_CLIENT: Model response blocked by Bedrock safety filters")
		return Response{}, fmt.Errorf("model response blocked by Bedrock safety filters")

	default:
		// Fallback if the model didn't specify a stop reason
		text, err := textFromOutput(out)
		if err != nil {
			return Response{}, fmt.Errorf("failed to extract text: %w", err)
		}
		calls, err := toolCallsFromOutput(out)
		if err != nil {
			return Response{}, fmt.Errorf("failed to parse tool calls: %w", err)
		}
		return Response{Content: text, ToolCalls: calls}, nil
	}
}

// buildToolSpec constructs a ToolSpecification for a tool.
func buildToolSpec(t Tool) (types.ToolSpecification, error) {
	// TODO: figure out what's causing the issue with tool schema marshalling forcing me to do this dance.
	// Pre-marshal the schema to JSON to ensure it uses the custom MarshalJSON method
	schemaJSON, err := json.Marshal(t.InputSchema)
	if err != nil {
		return types.ToolSpecification{}, fmt.Errorf("failed to marshal tool schema for %s: %w", t.Name, err)
	}

	// Parse it back to a map for the document system
	var schemaMap map[string]any
	if err := json.Unmarshal(schemaJSON, &schemaMap); err != nil {
		return types.ToolSpecification{}, fmt.Errorf("failed to unmarshal tool schema for %s: %w", t.Name, err)
	}

	return types.ToolSpecification{
		Name:        aws.String(t.Name),
		Description: aws.String(t.Description),
		InputSchema: &types.ToolInputSchemaMemberJson{
			Value: document.NewLazyDocument(schemaMap),
		},
	}, nil
}

// textFromOutput returns assistant text optimized for agent use:
// 1) If any text block looks like a single JSON object, return the last such block.
// 2) Else, if there's only one text block, return it.
// 3) Else, join all text blocks with '\n'.
func textFromOutput(out *bedrockruntime.ConverseOutput) (string, error) {
	if out == nil || out.Output == nil {
		return "", nil
	}

	msg, ok := out.Output.(*types.ConverseOutputMemberMessage)
	if !ok || msg == nil || len(msg.Value.Content) == 0 {
		return "", nil
	}

	texts := make([]string, 0, len(msg.Value.Content))
	for _, cb := range msg.Value.Content {
		if t, ok := cb.(*types.ContentBlockMemberText); ok && t != nil && t.Value != "" {
			texts = append(texts, t.Value)
		}
	}
	if len(texts) == 0 {
		return "", nil
	}

	// Prefer a single JSON object if present (typical for final agent output)
	for i := len(texts) - 1; i >= 0; i-- {
		s := strings.TrimSpace(texts[i])
		if len(s) > 1 && s[0] == '{' && s[len(s)-1] == '}' {
			return s, nil
		}
	}

	// Single block fast path
	if len(texts) == 1 {
		return texts[0], nil
	}

	// Join with one allocation
	total := len(texts) - 1 // for newlines
	for _, s := range texts {
		total += len(s)
	}

	var b strings.Builder
	b.Grow(total)

	for i, s := range texts {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s)
	}

	return b.String(), nil
}

// toolCallsFromOutput extracts tool uses emitted by the assistant.
func toolCallsFromOutput(out *bedrockruntime.ConverseOutput) ([]tools.Call, error) {
	var calls []tools.Call

	msg, ok := out.Output.(*types.ConverseOutputMemberMessage)
	if !ok || msg == nil || msg.Value.Content == nil {
		return calls, nil
	}

	for _, cb := range msg.Value.Content {
		tu, ok := cb.(*types.ContentBlockMemberToolUse)
		if !ok || tu == nil {
			continue
		}

		var input map[string]any
		if err := tu.Value.Input.UnmarshalSmithyDocument(&input); err != nil {
			input = map[string]any{}
		}

		// Normalize deeply instead of just top-level floats
		normalized := normalizeInput(input).(map[string]any)

		calls = append(calls, tools.Call{
			Name:      aws.ToString(tu.Value.Name),
			Input:     normalized,
			ToolUseID: aws.ToString(tu.Value.ToolUseId),
		})
	}

	return calls, nil
}

// normalizeInput recursively coerces types for safe downstream use.
func normalizeInput(val any) any {
	switch v := val.(type) {
	case float64:
		// Convert whole numbers like 2.0 → 2
		if v == float64(int(v)) {
			return int(v)
		}
		return v

	case string:
		// Check if it's a stringified JSON array or object
		var decoded any
		if json.Unmarshal([]byte(v), &decoded) == nil {
			return normalizeInput(decoded)
		}
		return v

	case []any:
		// Recursively clean each array element
		for i := range v {
			v[i] = normalizeInput(v[i])
		}
		return v

	case map[string]any:
		// Recursively clean each map value
		for key, val := range v {
			v[key] = normalizeInput(val)
		}
		return v

	default:
		return v
	}
}
