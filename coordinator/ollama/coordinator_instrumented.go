package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"pantryagent"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentedCoordinator is an instrumented version of the Coordinator with comprehensive observability metrics.
type InstrumentedCoordinator struct {
	llm           llmClient
	toolProvider  pantryagent.ToolProvider
	maxIterations int
	logger        pantryagent.CoordinationLogger
	tracer        trace.Tracer
	meter         metric.Meter
}

// NewInstrumentedCoordinator initializes a new instrumented coordinator.
func NewInstrumentedCoordinator(llm llmClient, tp pantryagent.ToolProvider, maxIter int, log pantryagent.CoordinationLogger, tracer trace.Tracer, meter metric.Meter) *InstrumentedCoordinator {
	return &InstrumentedCoordinator{
		llm:           llm,
		toolProvider:  tp,
		maxIterations: maxIter,
		logger:        log,
		tracer:        tracer,
		meter:         meter,
	}
}

// Run executes the coordination process for a given task with full instrumentation.
func (c *InstrumentedCoordinator) Run(ctx context.Context, task string) (string, error) {
	ctx, span := c.tracer.Start(ctx, "InstrumentedCoordinator.Run")
	defer span.End()

	slog.Info("COORDINATOR: Starting instrumented run", "task", task)

	// Initialize all metrics
	runsCounter, _ := c.meter.Int64Counter("coordinator_runs_total",
		metric.WithDescription("Total number of coordination runs started"))
	runsCompletedCounter, _ := c.meter.Int64Counter("coordinator_runs_completed_total",
		metric.WithDescription("Total number of coordination runs completed successfully"))
	runsFailedCounter, _ := c.meter.Int64Counter("coordinator_runs_failed_total",
		metric.WithDescription("Total number of coordination runs that failed"))
	toolCallsCounter, _ := c.meter.Int64Counter("tool_calls_total",
		metric.WithDescription("Total number of tool calls executed"))
	toolCallsFailedCounter, _ := c.meter.Int64Counter("tool_calls_failed_total",
		metric.WithDescription("Total number of tool calls that failed"))
	iterationCounter, _ := c.meter.Int64Counter("coordinator_iterations_total",
		metric.WithDescription("Total number of coordination iterations"))
	messageCounter, _ := c.meter.Int64Counter("coordinator_messages_total",
		metric.WithDescription("Total number of messages in coordination"))

	// Gauges
	promptSizeGauge, _ := c.meter.Int64Gauge("prompt_size_bytes",
		metric.WithDescription("Size of the prompt sent to LLM in bytes"))
	responseContentLengthGauge, _ := c.meter.Int64Gauge("response_content_length",
		metric.WithDescription("Length of the response content from LLM"))
	messagesInConversationGauge, _ := c.meter.Int64Gauge("messages_in_conversation",
		metric.WithDescription("Number of messages in the current conversation"))
	toolsAvailableGauge, _ := c.meter.Int64Gauge("tools_available_count",
		metric.WithDescription("Number of tools available to the coordinator"))

	// Histograms
	coordinationDurationHist, _ := c.meter.Float64Histogram("coordination_duration_seconds",
		metric.WithDescription("Total duration of coordination process in seconds"))
	iterationDurationHist, _ := c.meter.Float64Histogram("iteration_duration_seconds",
		metric.WithDescription("Duration of individual coordination iterations in seconds"))
	llmResponseTimeHist, _ := c.meter.Float64Histogram("llm_response_time_seconds",
		metric.WithDescription("Time taken to receive response from LLM in seconds"))
	toolExecutionTimeHist, _ := c.meter.Float64Histogram("tool_execution_time_seconds",
		metric.WithDescription("Time taken to execute individual tools in seconds"))

	// Ollama-specific counters
	toolDeduplicationsCounter, _ := c.meter.Int64Counter("tool_deduplications_total",
		metric.WithDescription("Total number of tool call deduplications performed"))
	missingToolResultsCounter, _ := c.meter.Int64Counter("missing_tool_results_total",
		metric.WithDescription("Total number of times required tool results were missing"))
	validFinalResponsesCounter, _ := c.meter.Int64Counter("valid_final_responses_total",
		metric.WithDescription("Total number of valid final responses received"))
	emptyResponsesCounter, _ := c.meter.Int64Counter("empty_responses_total",
		metric.WithDescription("Total number of empty responses received from LLM"))

	// Ollama-specific gauges
	toolCallsDeduplicatedGauge, _ := c.meter.Int64Gauge("tool_calls_deduplicated_count",
		metric.WithDescription("Number of tool calls removed by deduplication in latest iteration"))
	toolCallsOriginalGauge, _ := c.meter.Int64Gauge("tool_calls_original_count",
		metric.WithDescription("Original number of tool calls before deduplication"))

	// Record initial run
	runsCounter.Add(ctx, 1)

	// Set static gauges
	toolsAvailableGauge.Record(ctx, int64(len(c.toolProvider.GetTools())))

	prompt, err := NewPrompt(task, c.toolProvider)
	if err != nil {
		runsFailedCounter.Add(ctx, 1)
		span.SetStatus(codes.Error, "Failed to create prompt")
		span.RecordError(err)
		return "", fmt.Errorf("failed to apply system prompt: %w", err)
	}

	var finalOut string

	coordinationStartTime := time.Now()

	for iter := 0; iter < c.maxIterations; iter++ {
		iterationStartTime := time.Now()
		ctx, span := c.tracer.Start(ctx, fmt.Sprintf("InstrumentedCoordinator.Run.Iteration.%d", iter+1))
		defer span.End()

		iterationCounter.Add(ctx, 1)
		iterLog := pantryagent.IterationLog{Iteration: iter + 1, Timestamp: time.Now()}

		// Log prompt and record metrics
		promptJSON, merr := json.Marshal(prompt)
		var promptSize int
		if merr == nil {
			iterLog.LLMInput = string(promptJSON)
			promptSize = len(promptJSON)
			promptSizeGauge.Record(ctx, int64(promptSize))

			lastMessagePreview := func() string {
				if len(prompt.Messages) == 0 {
					return "no_content"
				}
				last := prompt.Messages[len(prompt.Messages)-1].Content
				if len(last) > 100 {
					return last[:97] + "..."
				}
				return last
			}

			slog.Info("COORDINATOR: Sending prompt to LLM",
				"iteration", iter+1,
				"messages_count", len(prompt.Messages),
				"tools_count", len(prompt.Tools),
				"prompt_size_bytes", promptSize,
				"last_message_preview", lastMessagePreview(),
			)

			messagesInConversationGauge.Record(ctx, int64(len(prompt.Messages)))

			span.AddEvent("Sending prompt to LLM", trace.WithAttributes(
				attribute.Int("iteration", iter+1),
				attribute.Int("messages_count", len(prompt.Messages)),
				attribute.Int("tools_count", len(prompt.Tools)),
				attribute.Int("prompt_size_bytes", promptSize),
				attribute.String("last_message_preview", lastMessagePreview()),
			))
		}

		// 1) Invoke model
		llmStartTime := time.Now()
		res, err := c.llm.Invoke(ctx, prompt)
		llmDuration := time.Since(llmStartTime)
		llmResponseTimeHist.Record(ctx, llmDuration.Seconds())

		if err != nil {
			iterLog.Error = err.Error()
			c.logIteration(iterLog)
			runsFailedCounter.Add(ctx, 1)
			span.SetStatus(codes.Error, "LLM invoke failed")
			span.RecordError(err)
			return finalOut, fmt.Errorf("failed to invoke LLM: %w", err)
		}
		iterLog.LLMOutput = res

		span.AddEvent("LLM response received", trace.WithAttributes(
			attribute.Int("response_content_length", len(res.Content)),
			attribute.Int("response_tool_calls_length", len(res.ToolCalls)),
			attribute.Float64("llm_response_time_seconds", llmDuration.Seconds()),
		))

		responseContentLengthGauge.Record(ctx, int64(len(res.Content)))
		messageCounter.Add(ctx, int64(len(prompt.Messages)+1)) // +1 for the response message

		slog.Info("COORDINATOR: LLM response received",
			"iteration", iter+1,
			"content_length", len(res.Content),
			"tool_calls", len(res.ToolCalls),
			"llm_response_time_ms", llmDuration.Milliseconds(),
		)

		// 2a) Final JSON path (no tool calls)
		if len(res.ToolCalls) == 0 && res.Content != "" {
			// Accept final only if we have pantry_get and recipe_get results in history
			if !(prompt.HasToolResult("pantry_get") && prompt.HasToolResult("recipe_get")) {
				missingToolResultsCounter.Add(ctx, 1)
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

			// We have the required tool results; accept the model's final JSON as-is.
			validFinalResponsesCounter.Add(ctx, 1)
			runsCompletedCounter.Add(ctx, 1)
			slog.Info("COORDINATOR: Content looks final; ending run", "iteration", iter+1)
			finalOut = res.Content
			iterationDuration := time.Since(iterationStartTime)
			iterationDurationHist.Record(ctx, iterationDuration.Seconds())
			c.logIteration(iterLog)
			coordinationDuration := time.Since(coordinationStartTime)
			coordinationDurationHist.Record(ctx, coordinationDuration.Seconds())

			span.AddEvent("Valid final response accepted")
			break
		}

		// 2b) Tool-call path
		if len(res.ToolCalls) == 0 && res.Content == "" {
			emptyResponsesCounter.Add(ctx, 1)
			err := fmt.Errorf("no tool_calls and no final content")
			iterLog.Error = err.Error()
			c.logIteration(iterLog)
			runsFailedCounter.Add(ctx, 1)
			span.SetStatus(codes.Error, "Empty response received")
			span.RecordError(err)
			return "", err
		}

		var toolCallLogs []pantryagent.ToolCallLog

		// Record original tool call count before deduplication
		originalToolCallCount := len(res.ToolCalls)
		toolCallsOriginalGauge.Record(ctx, int64(originalToolCallCount))

		toolCalls := dedupeToolCalls(res.ToolCalls)
		deduplicatedCount := originalToolCallCount - len(toolCalls)
		toolCallsDeduplicatedGauge.Record(ctx, int64(deduplicatedCount))

		if deduplicatedCount > 0 {
			toolDeduplicationsCounter.Add(ctx, int64(deduplicatedCount))
			slog.Info("COORDINATOR: Deduped tool calls", "requested", originalToolCallCount, "kept", len(toolCalls), "deduplicated", deduplicatedCount)

			span.AddEvent("Tool calls deduplicated", trace.WithAttributes(
				attribute.Int("original_count", originalToolCallCount),
				attribute.Int("deduplicated_count", deduplicatedCount),
				attribute.Int("final_count", len(toolCalls)),
			))
		}

		for _, call := range toolCalls {
			slog.Info("COORDINATOR: Handling tool call", "name", call.Name, "iteration", iter+1)

			toolCallsCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("tool_name", call.Name),
			))

			toolLog := pantryagent.ToolCallLog{Name: call.Name, Input: call.Args}

			tool, err := c.toolProvider.GetTool(call.Name)
			if err != nil {
				toolCallsFailedCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("tool_name", call.Name),
					attribute.String("error_type", "tool_not_found"),
				))
				toolLog.Error = err.Error()
				toolCallLogs = append(toolCallLogs, toolLog)
				iterLog.ToolCalls = toolCallLogs
				c.logIteration(iterLog)
				runsFailedCounter.Add(ctx, 1)
				span.SetStatus(codes.Error, "Tool not found")
				span.RecordError(err)
				return finalOut, fmt.Errorf("failed to get tool %q: %w", call.Name, err)
			}

			toolStartTime := time.Now()
			result, err := tool.Run(ctx, call.Args)
			toolDuration := time.Since(toolStartTime)
			toolExecutionTimeHist.Record(ctx, toolDuration.Seconds(), metric.WithAttributes(
				attribute.String("tool_name", call.Name),
			))

			if err != nil {
				toolCallsFailedCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("tool_name", call.Name),
					attribute.String("error_type", "tool_execution_failed"),
				))
				toolLog.Error = err.Error()
				toolCallLogs = append(toolCallLogs, toolLog)
				iterLog.ToolCalls = toolCallLogs
				c.logIteration(iterLog)
				runsFailedCounter.Add(ctx, 1)
				span.SetStatus(codes.Error, "Tool execution failed")
				span.RecordError(err)
				return "", fmt.Errorf("failed to run tool %q: %w", call.Name, err)
			}

			toolLog.Output = result
			toolCallLogs = append(toolCallLogs, toolLog)

			payload, err := json.Marshal(result)
			if err != nil {
				iterLog.Error = fmt.Sprintf("failed to marshal tool result: %v", err)
				c.logIteration(iterLog)
				runsFailedCounter.Add(ctx, 1)
				span.SetStatus(codes.Error, "Failed to marshal tool result")
				span.RecordError(err)
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

			span.AddEvent("Tool executed successfully", trace.WithAttributes(
				attribute.String("tool_name", call.Name),
				attribute.Float64("tool_execution_time_seconds", toolDuration.Seconds()),
			))

			slog.Info("COORDINATOR: Tool executed, appended message", "name", call.Name, "iteration", iter+1)
		}

		iterLog.ToolCalls = toolCallLogs
		iterationDuration := time.Since(iterationStartTime)
		iterationDurationHist.Record(ctx, iterationDuration.Seconds())
		c.logIteration(iterLog)
	}

	coordinationDuration := time.Since(coordinationStartTime)
	coordinationDurationHist.Record(ctx, coordinationDuration.Seconds())

	if finalOut == "" {
		runsFailedCounter.Add(ctx, 1)
		span.SetStatus(codes.Error, "Max iterations reached without final output")
	}

	return finalOut, nil
}

// logIteration logs a step using the configured logger, handling errors gracefully
func (c *InstrumentedCoordinator) logIteration(iteration pantryagent.IterationLog) {
	if c.logger != nil {
		if err := c.logger.LogIteration(iteration); err != nil {
			slog.Error("Failed to log coordination iteration", "error", err, "iteration", iteration.Iteration)
		}
	}
}
