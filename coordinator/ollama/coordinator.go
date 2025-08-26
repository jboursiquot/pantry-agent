package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"pantryagent"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
)

// Coordinator is responsible for managing the interaction between the LLM, tools, and output channel.
type Coordinator struct {
	llm            llmClient
	toolProvider   pantryagent.ToolProvider
	maxIterations  int
	logger         pantryagent.CoordinationLogger
	tracerProvider *trace.TracerProvider
}

// llmClient interface for ollama-specific client
type llmClient interface {
	Invoke(ctx context.Context, prompt Prompt) (Response, error)
}

// NewCoordinator initializes a new coordinator.
func NewCoordinator(llm llmClient, tp pantryagent.ToolProvider, maxIter int, log pantryagent.CoordinationLogger, tracerProvider *trace.TracerProvider) *Coordinator {
	return &Coordinator{
		llm:            llm,
		toolProvider:   tp,
		maxIterations:  maxIter,
		logger:         log,
		tracerProvider: tracerProvider,
	}
}

// Run executes the coordination process for a given task.
func (c *Coordinator) Run(ctx context.Context, task string) (string, error) {
	ctx, span := otel.Tracer(pantryagent.TracerNameOllama).Start(ctx, "Coordinator.Run")
	defer span.End()

	slog.Info("COORDINATOR: Starting run", "task", task)

	prompt, err := NewPrompt(task, c.toolProvider)
	if err != nil {
		return "", fmt.Errorf("failed to apply system prompt: %w", err)
	}

	var finalOut string

	for iter := 0; iter < c.maxIterations; iter++ {
		iterLog := pantryagent.IterationLog{Iteration: iter + 1, Timestamp: time.Now()}

		// Log prompt
		if b, merr := json.Marshal(prompt); merr == nil {
			iterLog.LLMInput = string(b)
			slog.Info("COORDINATOR: Sending prompt to LLM",
				"iteration", iter+1,
				"messages_count", len(prompt.Messages),
				"tools_count", len(prompt.Tools),
				"prompt_size_bytes", len(b),
				"last_message_preview", func() string {
					if len(prompt.Messages) == 0 {
						return "no_content"
					}
					last := prompt.Messages[len(prompt.Messages)-1].Content
					if len(last) > 100 {
						return last[:97] + "..."
					}
					return last
				}(),
			)
		}

		// 1) Invoke model
		res, err := c.llm.Invoke(ctx, prompt)
		if err != nil {
			iterLog.Error = err.Error()
			c.logIteration(iterLog)
			return finalOut, fmt.Errorf("failed to invoke LLM: %w", err)
		}
		iterLog.LLMOutput = res

		slog.Info("COORDINATOR: LLM response received",
			"iteration", iter+1,
			"content_length", len(res.Content),
			"tool_calls", len(res.ToolCalls),
		)

		// 2a) Final JSON path (no tool calls)
		if len(res.ToolCalls) == 0 && res.Content != "" {
			// Accept final only if we have pantry_get and recipe_get results in history
			if !(prompt.HasToolResult("pantry_get") && prompt.HasToolResult("recipe_get")) {
				slog.Info("COORDINATOR: Missing required tool results; nudging model to call tools", "iteration", iter+1)

				// Nudge the model to call tools natively
				prompt.Messages = append(prompt.Messages,
					Message{
						Role:    "user",
						Content: "Before finalizing, call pantry_get (with current_day) and recipe_get (optionally with meal_types). Then use those results and return ONLY the final JSON object.",
					},
				)
				c.logIteration(iterLog)
				continue
			}

			// We have the required tool results; accept the model’s final JSON as-is.
			slog.Info("COORDINATOR: Content looks final; ending run", "iteration", iter+1)
			finalOut = res.Content
			c.logIteration(iterLog)
			break
		}

		// 2b) Tool-call path
		if len(res.ToolCalls) == 0 && res.Content == "" {
			err := fmt.Errorf("no tool_calls and no final content")
			iterLog.Error = err.Error()
			c.logIteration(iterLog)
			return "", err
		}

		var toolCallLogs []pantryagent.ToolCallLog

		toolCalls := dedupeToolCalls(res.ToolCalls)
		if len(toolCalls) < len(res.ToolCalls) {
			slog.Info("COORDINATOR: Deduped tool calls", "requested", len(res.ToolCalls), "kept", len(toolCalls))
		}

		for _, call := range toolCalls {
			slog.Info("COORDINATOR: Handling tool call", "name", call.Name, "iteration", iter+1)

			toolLog := pantryagent.ToolCallLog{Name: call.Name, Input: call.Args}

			tool, err := c.toolProvider.GetTool(call.Name)
			if err != nil {
				toolLog.Error = err.Error()
				toolCallLogs = append(toolCallLogs, toolLog)
				iterLog.ToolCalls = toolCallLogs
				c.logIteration(iterLog)
				return finalOut, fmt.Errorf("failed to get tool %q: %w", call.Name, err)
			}

			result, err := tool.Run(ctx, call.Args)
			if err != nil {
				toolLog.Error = err.Error()
				toolCallLogs = append(toolCallLogs, toolLog)
				iterLog.ToolCalls = toolCallLogs
				c.logIteration(iterLog)
				return "", fmt.Errorf("failed to run tool %q: %w", call.Name, err)
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
					Role:    "tool",
					Name:    tool.Name(),
					Content: string(payload),
				},
			)

			slog.Info("COORDINATOR: Tool executed, appended message", "name", call.Name, "iteration", iter+1)
		}

		iterLog.ToolCalls = toolCallLogs
		c.logIteration(iterLog)
	}

	return finalOut, nil
}

// dedupeToolCalls keeps only the first call per tool name (or name+args hash).
// This exists because the model may be "eager" and call the same tool multiple times with the same arguments.
func dedupeToolCalls(calls []ToolCall) []ToolCall {
	seen := map[string]bool{}
	out := make([]ToolCall, 0, len(calls))
	for _, c := range calls {
		// key := c.Name // simplest: per tool name
		b, _ := json.Marshal(c.Args)
		key := c.Name + ":" + string(b) // per (name,args) uniqueness
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

// logIteration logs a step using the configured logger, handling errors gracefully
func (c *Coordinator) logIteration(iteration pantryagent.IterationLog) {
	if c.logger != nil {
		if err := c.logger.LogIteration(iteration); err != nil {
			slog.Error("Failed to log coordination iteration", "error", err, "iteration", iteration.Iteration)
		}
	}
}
