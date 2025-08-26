package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"pantryagent"
)

// Coordinator is responsible for managing the interaction between the LLM, tools, and output channel.
type Coordinator struct {
	llm           llmClient
	toolProvider  pantryagent.ToolProvider
	maxIterations int
	logger        pantryagent.CoordinationLogger
}

// llmClient interface for mock-specific client. It's fake and just returns canned responses.
type llmClient interface {
	Invoke(ctx context.Context, prompt Prompt) (Response, error)
}

// NewCoordinator initializes a new coordinator.
func NewCoordinator(llm llmClient, tp pantryagent.ToolProvider, maxIter int, log pantryagent.CoordinationLogger) *Coordinator {
	return &Coordinator{
		llm:           llm,
		toolProvider:  tp,
		maxIterations: maxIter,
		logger:        log,
	}
}

// Run executes the coordination process for a given task.
func (c *Coordinator) Run(ctx context.Context, task string) (string, error) {
	slog.Info("COORDINATOR: Starting run", "task", task)

	prompt, err := NewPrompt(task, c.toolProvider)
	if err != nil {
		return "", fmt.Errorf("failed to apply system prompt: %w", err)
	}

	var finalOut string

	for iter := 0; iter < c.maxIterations; iter++ {
		iterLog := pantryagent.IterationLog{Iteration: iter + 1, Timestamp: time.Now()}

		// Serialize the prompt for debugging
		promptJSON, err := json.Marshal(prompt)
		if err != nil {
			err := fmt.Errorf("failed to marshal prompt: %w", err)
			iterLog.Error = err.Error()
			c.logIteration(iterLog)
			return finalOut, err
		}
		iterLog.LLMInput = string(promptJSON)
		promptSize := len(promptJSON)

		lastMessagePreview := func() string {
			if len(prompt.Messages) > 0 {
				lastMsg := prompt.Messages[len(prompt.Messages)-1]
				if len(lastMsg.Content) > 0 && len(lastMsg.Content[0].Text) > 0 {
					text := lastMsg.Content[0].Text
					if len(text) > 100 {
						return text[:97] + "..."
					}
					return text
				}
			}
			return "no_content"
		}

		slog.Info("COORDINATOR: Sending prompt to LLM",
			"iteration", iter+1,
			"messages_count", len(prompt.Messages),
			"tools_count", len(prompt.Tools),
			"prompt_size_bytes", promptSize,
			"last_message_preview", lastMessagePreview(),
		)

		// Invoke model
		res, err := c.llm.Invoke(ctx, prompt)
		if err != nil {
			iterLog.Error = err.Error()
			c.logIteration(iterLog)
			return finalOut, fmt.Errorf("failed to invoke LLM: %w", err)
		}
		iterLog.LLMOutput = res

		// Parse model output
		contentLengthBeforeParsing := len(res.Content)
		if err := res.ParseModelOutput(); err != nil {
			iterLog.Error = fmt.Sprintf("failed to parse model output: %v", err)
			c.logIteration(iterLog)
			return finalOut, fmt.Errorf("failed to parse model output: %w", err)
		}

		slog.Info("COORDINATOR: LLM response received",
			"iteration", iter+1,
			"content_length", contentLengthBeforeParsing,
			"tool_calls", len(res.ToolCalls),
		)

		// Final? (only accept if pantry get + recipe get have occurred)
		if res.Content != "" {
			usedPantryGet := prompt.HasToolResultInContent("pantry_get")
			usedRecipeGet := prompt.HasToolResultInContent("recipe_get")

			if !(usedPantryGet && usedRecipeGet) {
				// Nudge the model back to tool planning
				correction := `{
					"tool_calls": [
						{ "name": "pantry_get", "input": { "current_day": 0 } },
						{ "name": "recipe_get", "input": { "meal_types": ["dinner"] } }
					]
				}`

				prompt.Messages = append(prompt.Messages,
					Message{
						Role: "user",
						Content: []MessagePart{{
							Type: "text",
							Text: `Your last output was a final plan but you did not retrieve the pantry nor the recipe get. Use "pantry_get" for pantry data and "recipe_get" for recipes before finalizing.`,
						}},
					},
					Message{
						Role:    "assistant",
						Content: []MessagePart{{Type: "text", Text: correction}},
					},
				)

				c.logIteration(iterLog)
				continue
			}

			slog.Info("COORDINATOR: Content is final output, ending run", "iteration", iter+1, "content_length", len(res.Content))

			finalOut = res.Content
			c.logIteration(iterLog)
			break
		}

		// Execute tool calls
		if len(res.ToolCalls) == 0 {
			err := fmt.Errorf("COORDINATOR: no tool_calls and no final in response")
			iterLog.Error = err.Error()
			c.logIteration(iterLog)
			return finalOut, err
		}

		var toolCallLogs []pantryagent.ToolCallLog
		for _, call := range res.ToolCalls {
			slog.Info("COORDINATOR: Handling tool call", "name", call.Name, "iteration", iter+1)

			toolLog := pantryagent.ToolCallLog{Name: call.Name, Input: call.Input}

			tool, err := c.toolProvider.GetTool(call.Name)
			if err != nil {
				toolLog.Error = err.Error()
				toolCallLogs = append(toolCallLogs, toolLog)
				iterLog.ToolCalls = toolCallLogs
				c.logIteration(iterLog)
				return finalOut, fmt.Errorf("failed to get tool %q: %w", call.Name, err)
			}

			result, err := tool.Run(ctx, call.Input)
			if err != nil {
				toolLog.Error = err.Error()
				toolCallLogs = append(toolCallLogs, toolLog)
				iterLog.ToolCalls = toolCallLogs
				c.logIteration(iterLog)
				return finalOut, fmt.Errorf("failed to run tool %q: %w", call.Name, err)
			}

			toolLog.Output = result
			toolCallLogs = append(toolCallLogs, toolLog)

			payload, err := json.Marshal(result)
			if err != nil {
				iterLog.Error = fmt.Sprintf("failed to marshal tool result: %v", err)
				c.logIteration(iterLog)
				return finalOut, fmt.Errorf("failed to marshal tool result: %w", err)
			}

			prompt.Messages = append(
				prompt.Messages,
				Message{
					Role: "user",
					Content: []MessagePart{
						{Type: "text", Text: fmt.Sprintf(`{"tool_result":"%s","data":%s}`, tool.Name(), string(payload))},
					},
				},
			)

			slog.Info("COORDINATOR: Tool executed, appended message", "name", call.Name, "iteration", iter+1)
		}

		iterLog.ToolCalls = toolCallLogs
		c.logIteration(iterLog)
	}

	return finalOut, nil
}

// logIteration logs a step using the configured logger, handling errors gracefully
func (c *Coordinator) logIteration(iteration pantryagent.IterationLog) {
	if c.logger != nil {
		if err := c.logger.LogIteration(iteration); err != nil {
			slog.Error("Failed to log coordination iteration", "error", err, "iteration", iteration.Iteration)
		}
	}
}
