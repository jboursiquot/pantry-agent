package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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
	pantry        map[string]any
	recipes       []any
	tracer        trace.Tracer
	meter         metric.Meter
}

// NewInstrumentedCoordinator initializes a new instrumented coordinator.
func NewInstrumentedCoordinator(llm llmClient, toolRegistry pantryagent.ToolProvider, pantryData map[string]any, recipeData []any, maxIterations int, logger pantryagent.CoordinationLogger, tracer trace.Tracer, meter metric.Meter) *InstrumentedCoordinator {
	return &InstrumentedCoordinator{
		llm:           llm,
		toolProvider:  toolRegistry,
		maxIterations: maxIterations,
		logger:        logger,
		pantry:        pantryData,
		recipes:       recipeData,
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
	pantryIngredientsGauge, _ := c.meter.Int64Gauge("pantry_ingredients_count",
		metric.WithDescription("Number of ingredients in the pantry"))
	recipesAvailableGauge, _ := c.meter.Int64Gauge("recipes_available_count",
		metric.WithDescription("Number of recipes available"))

	// Histograms
	coordinationDurationHist, _ := c.meter.Float64Histogram("coordination_duration_seconds",
		metric.WithDescription("Total duration of coordination process in seconds"))
	iterationDurationHist, _ := c.meter.Float64Histogram("iteration_duration_seconds",
		metric.WithDescription("Duration of individual coordination iterations in seconds"))
	llmResponseTimeHist, _ := c.meter.Float64Histogram("llm_response_time_seconds",
		metric.WithDescription("Time taken to receive response from LLM in seconds"))
	toolExecutionTimeHist, _ := c.meter.Float64Histogram("tool_execution_time_seconds",
		metric.WithDescription("Time taken to execute individual tools in seconds"))
	feasibilityCheckTimeHist, _ := c.meter.Float64Histogram("feasibility_check_time_seconds",
		metric.WithDescription("Time taken to perform feasibility checks in seconds"))

	// Bedrock-specific counters
	feasibilityChecksCounter, _ := c.meter.Int64Counter("feasibility_checks_total",
		metric.WithDescription("Total number of feasibility checks performed"))
	feasibilityChecksFailedCounter, _ := c.meter.Int64Counter("feasibility_checks_failed_total",
		metric.WithDescription("Total number of feasibility checks that failed"))
	toolRepetitionPreventedCounter, _ := c.meter.Int64Counter("tool_repetition_prevented_total",
		metric.WithDescription("Total number of times tool repetition was prevented"))
	validFinalPlansCounter, _ := c.meter.Int64Counter("valid_final_plans_total",
		metric.WithDescription("Total number of valid final plans generated"))
	invalidFinalPlansCounter, _ := c.meter.Int64Counter("invalid_final_plans_total",
		metric.WithDescription("Total number of invalid final plans attempted"))
	mealPlanValidationErrorsCounter, _ := c.meter.Int64Counter("meal_plan_validation_errors_total",
		metric.WithDescription("Total number of meal plan validation errors"))

	// Bedrock-specific gauges
	toolRepetitionCountGauge, _ := c.meter.Int64Gauge("tool_repetition_count",
		metric.WithDescription("Current count of tool repetitions"))
	feasibilityProblemsGauge, _ := c.meter.Int64Gauge("feasibility_problems_count",
		metric.WithDescription("Number of feasibility problems in the latest check"))

	// Record initial run
	runsCounter.Add(ctx, 1)

	// Set static gauges
	toolsAvailableGauge.Record(ctx, int64(len(c.toolProvider.GetTools())))
	if c.pantry != nil {
		if ingredients, ok := c.pantry["ingredients"].([]any); ok {
			pantryIngredientsGauge.Record(ctx, int64(len(ingredients)))
		}
	}
	recipesAvailableGauge.Record(ctx, int64(len(c.recipes)))

	prompt, err := NewPrompt(task, c.toolProvider)
	if err != nil {
		runsFailedCounter.Add(ctx, 1)
		span.SetStatus(codes.Error, "Failed to create prompt")
		span.RecordError(err)
		return "", fmt.Errorf("failed to apply system prompt: %w", err)
	}

	var finalOut string
	toolsAlreadyCalled := make(map[string]int) // Track how many times each tool has been called

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
				text := "no content"
				if len(prompt.Messages) == 0 {
					return text
				}
				last := prompt.Messages[len(prompt.Messages)-1]
				if len(last.Content) > 0 && len(last.Content[0].Text) > 0 {
					text = last.Content[0].Text
					if len(text) > 100 {
						text = text[:97] + "..."
					}
				}
				return text
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
			return "", fmt.Errorf("invoke failed: %w", err)
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

		// If the assistant returned no tool calls, treat content as a potential final plan.
		if len(res.ToolCalls) == 0 {
			slog.Info("COORDINATOR: No tool calls; attempting to treat output as final plan", "iteration", iter+1, "content_length", len(res.Content))
			finalJSON := strings.TrimSpace(res.Content)

			// Validate final JSON structure.
			if finalJSON == "" || !strings.HasPrefix(finalJSON, "{") || !strings.HasSuffix(finalJSON, "}") {
				slog.Info("COORDINATOR: Output is not valid JSON format", "iteration", iter+1, "starts_with_brace", strings.HasPrefix(finalJSON, "{"), "ends_with_brace", strings.HasSuffix(finalJSON, "}"))
				invalidFinalPlansCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("validation_error", "not_json_format"),
				))
				// Not a final plan; ask it to proceed with tools for interactive context.
				slog.Info("COORDINATOR: Requesting tools to build interactive context", "iteration", iter+1)
				prompt.Messages = append(prompt.Messages, Message{
					Role: "user",
					Content: []MessagePart{{
						Type: "text",
						Text: `{"tool_calls":[{"name":"pantry_get","input":{"current_day":0}},{"name":"recipe_get","input":{"meal_types":["dinner"]}}]}`,
					}},
				})
				c.logIteration(iterLog)
				continue
			}

			// Validate shape with MealPlan type.
			var mealPlan pantryagent.MealPlan
			if err := json.Unmarshal([]byte(finalJSON), &mealPlan); err != nil || !mealPlan.IsValid() {
				slog.Info("COORDINATOR: Final JSON failed schema validation", "error", err, "iteration", iter+1)
				invalidFinalPlansCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("validation_error", "schema_validation_failed"),
				))
				mealPlanValidationErrorsCounter.Add(ctx, 1)
				// Ask the model to restate as valid JSON per schema.
				msg := map[string]any{
					"error":  "invalid_final_json",
					"reason": fmt.Sprintf("parse/shape error: %v", err),
				}
				b, _ := json.Marshal(msg)
				prompt.Messages = append(prompt.Messages, Message{
					Role:    "user",
					Content: []MessagePart{{Type: "text", Text: string(b)}},
				})
				c.logIteration(iterLog)
				continue
			}

			// Feasibility check against static pantry/recipes data.
			slog.Info("COORDINATOR: Running feasibility check",
				"iteration", iter+1,
				"pantry_ingredients_count", func() int {
					if c.pantry != nil {
						if ingredients, ok := c.pantry["ingredients"].([]any); ok {
							return len(ingredients)
						}
					}
					return 0
				}(),
				"recipes_count", len(c.recipes))

			feasibilityChecksCounter.Add(ctx, 1)
			feasibilityStartTime := time.Now()
			feasible, probs, ferr := c.checkFeasible(finalJSON)
			feasibilityDuration := time.Since(feasibilityStartTime)
			feasibilityCheckTimeHist.Record(ctx, feasibilityDuration.Seconds())

			if ferr != nil {
				slog.Error("COORDINATOR: Feasibility check failed", "error", ferr, "iteration", iter+1)
				feasibilityChecksFailedCounter.Add(ctx, 1)
				msg := map[string]any{
					"error":  "feasibility_check_failed",
					"reason": ferr.Error(),
				}
				b, _ := json.Marshal(msg)
				prompt.Messages = append(prompt.Messages, Message{
					Role:    "user",
					Content: []MessagePart{{Type: "text", Text: string(b)}},
				})
				c.logIteration(iterLog)
				continue
			}

			feasibilityProblemsGauge.Record(ctx, int64(len(probs)))

			if !feasible {
				// Tell the model exactly why and ask it to re-plan (no mutations in this project).
				slog.Warn("COORDINATOR: Feasibility check failed", "iteration", iter+1, "problems", probs)
				feasibilityChecksFailedCounter.Add(ctx, 1)
				invalidFinalPlansCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("validation_error", "feasibility_check_failed"),
					attribute.Int("problem_count", len(probs)),
				))
				msg := map[string]any{
					"error":   "infeasible_plan",
					"details": probs,
					"hint":    "Revise recipe choices so all required ingredients (with units) fit the pantry; then re-send final JSON.",
				}
				b, _ := json.Marshal(msg)
				prompt.Messages = append(prompt.Messages, Message{
					Role:    "user",
					Content: []MessagePart{{Type: "text", Text: string(b)}},
				})
				iterLog.Error = "infeasible final plan"
				c.logIteration(iterLog)
				continue
			}

			// Feasible — accept and finish.
			validFinalPlansCounter.Add(ctx, 1)
			runsCompletedCounter.Add(ctx, 1)
			finalOut = finalJSON
			iterationDuration := time.Since(iterationStartTime)
			iterationDurationHist.Record(ctx, iterationDuration.Seconds())
			c.logIteration(iterLog)
			coordinationDuration := time.Since(coordinationStartTime)
			coordinationDurationHist.Record(ctx, coordinationDuration.Seconds())

			span.AddEvent("Valid final plan accepted", trace.WithAttributes(
				attribute.Int("feasibility_problems_count", len(probs)),
				attribute.Float64("feasibility_check_time_seconds", feasibilityDuration.Seconds()),
			))
			break
		}

		// Model requested tool calls: check for excessive repetition first
		var hasExcessiveRepetition bool
		var maxRepetitionCount int
		for _, call := range res.ToolCalls {
			toolsAlreadyCalled[call.Name]++

			if toolsAlreadyCalled[call.Name] > maxRepetitionCount {
				maxRepetitionCount = toolsAlreadyCalled[call.Name]
			}

			// Detect excessive repetition of data-gathering tools
			if (call.Name == "pantry_get" || call.Name == "recipe_get") && toolsAlreadyCalled[call.Name] > 2 {
				slog.Warn("COORDINATOR: Excessive tool repetition detected", "tool", call.Name, "count", toolsAlreadyCalled[call.Name], "iteration", iter+1)
				hasExcessiveRepetition = true
				break
			}
		}

		toolRepetitionCountGauge.Record(ctx, int64(maxRepetitionCount))

		if hasExcessiveRepetition {
			toolRepetitionPreventedCounter.Add(ctx, 1)
			// Provide more direct guidance without executing tools
			msg := map[string]any{
				"error": "excessive_tool_repetition",
				"hint":  "You've already gathered pantry and recipe data multiple times. Use the existing data to select feasible recipes that fit the available ingredients and provide the final JSON plan directly.",
			}
			b, _ := json.Marshal(msg)
			prompt.Messages = append(prompt.Messages, Message{
				Role:    "user",
				Content: []MessagePart{{Type: "text", Text: string(b)}},
			})
			iterLog.Error = "excessive tool repetition"
			c.logIteration(iterLog)
			continue
		}

		// Normal tool execution path
		assistantMsg := Message{Role: "assistant", Content: MessageParts{}}
		if res.Content != "" {
			assistantMsg.Content = append(assistantMsg.Content, MessagePart{Type: "text", Text: res.Content})
		}

		for _, call := range res.ToolCalls {
			slog.Info("COORDINATOR: Handling tool call", "name", call.Name, "iteration", iter+1)
			assistantMsg.Content = append(assistantMsg.Content, MessagePart{
				Type:      "tool_use",
				ToolUseID: call.ToolUseID,
				ToolName:  call.Name,
				Data:      call.Input,
			})
		}

		prompt.Messages = append(prompt.Messages, assistantMsg)

		var toolCallLogs []pantryagent.ToolCallLog
		var toolResults []ToolResult

		for _, call := range res.ToolCalls {
			toolCallsCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("tool_name", call.Name),
			))

			tlog := pantryagent.ToolCallLog{Name: call.Name, Input: call.Input}
			tool, gerr := c.toolProvider.GetTool(call.Name)
			if gerr != nil {
				toolCallsFailedCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("tool_name", call.Name),
					attribute.String("error_type", "tool_not_found"),
				))
				tlog.Error = gerr.Error()
				toolCallLogs = append(toolCallLogs, tlog)
				toolResults = append(toolResults, ToolResult{
					ToolUseID: call.ToolUseID,
					ToolName:  call.Name,
					Data:      map[string]any{"error": fmt.Sprintf("tool %q not found: %v", call.Name, gerr)},
				})
				continue
			}

			toolStartTime := time.Now()
			result, rerr := tool.Run(ctx, call.Input)
			toolDuration := time.Since(toolStartTime)
			toolExecutionTimeHist.Record(ctx, toolDuration.Seconds(), metric.WithAttributes(
				attribute.String("tool_name", call.Name),
			))

			if rerr != nil {
				toolCallsFailedCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("tool_name", call.Name),
					attribute.String("error_type", "tool_execution_failed"),
				))
				tlog.Error = rerr.Error()
				toolCallLogs = append(toolCallLogs, tlog)
				toolResults = append(toolResults, ToolResult{
					ToolUseID: call.ToolUseID,
					ToolName:  tool.Name(),
					Data:      map[string]any{"error": fmt.Sprintf("tool %q failed: %v", call.Name, rerr)},
				})
				continue
			}

			tlog.Output = result
			toolCallLogs = append(toolCallLogs, tlog)
			toolResults = append(toolResults, ToolResult{
				ToolUseID: call.ToolUseID,
				ToolName:  tool.Name(),
				Data:      result,
			})

			span.AddEvent("Tool executed successfully", trace.WithAttributes(
				attribute.String("tool_name", call.Name),
				attribute.Float64("tool_execution_time_seconds", toolDuration.Seconds()),
			))
		}

		if len(toolResults) > 0 {
			prompt.Messages = append(prompt.Messages, NewToolResultMessage(toolResults))
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

// checkFeasible validates that a candidate final JSON meal plan is doable with the
// most recent pantry and recipe catalog (no unit conversions, no shortages).
func (c *InstrumentedCoordinator) checkFeasible(finalJSON string) (ok bool, problems []string, err error) {
	const eps = 1e-9

	var plan pantryagent.MealPlan
	if err := json.Unmarshal([]byte(finalJSON), &plan); err != nil {
		return false, nil, fmt.Errorf("parse plan: %w", err)
	}

	if len(plan.DaysPlanned) == 0 {
		return false, []string{"days_planned must be non-empty"}, nil
	}

	// Helpers
	ltrim := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	getStr := func(m map[string]any, k string) string {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	getNum := func(m map[string]any, k string) float64 {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case float64:
				return t
			case float32:
				return float64(t)
			case int:
				return float64(t)
			case int32:
				return float64(t)
			case int64:
				return float64(t)
			case json.Number:
				f, _ := t.Float64()
				return f
			}
		}
		return 0
	}
	getBool := func(m map[string]any, k string) bool {
		if v, ok := m[k]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
		return false
	}

	// ---- index pantry -> name(lower) -> (qty, unit) ----
	type pslot struct {
		qty  float64
		unit string
	}
	pantryIdx := map[string]pslot{}
	if p, ok := c.pantry["ingredients"]; ok {
		if pantryIngs, ok := p.([]any); ok {
			for _, it := range pantryIngs {
				m, _ := it.(map[string]any)
				name := ltrim(getStr(m, "name"))
				if name == "" {
					continue
				}
				pantryIdx[name] = pslot{
					qty:  getNum(m, "qty"),
					unit: strings.TrimSpace(getStr(m, "unit")),
				}
			}
		}
	}

	// ---- index recipes by id -> (baseServings, ingredients[]) ----
	type need struct {
		name     string
		qty      float64
		unit     string
		optional bool
	}
	type rec struct {
		baseServ int
		needs    []need
	}
	recByID := map[string]rec{}
	for _, recipe := range c.recipes {
		r, ok := recipe.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(getStr(r, "id"))
		if id == "" {
			continue
		}
		base := int(getNum(r, "servings"))
		if base <= 0 {
			problems = append(problems, fmt.Sprintf("recipe %q has invalid base servings", id))
			base = 1
		}
		var needs []need
		if ingRaw, ok := r["ingredients"]; ok {
			if arr, ok := ingRaw.([]any); ok {
				for _, x := range arr {
					im, _ := x.(map[string]any)
					nm := ltrim(getStr(im, "name"))
					un := strings.TrimSpace(getStr(im, "unit"))
					qv := getNum(im, "qty")
					op := getBool(im, "optional")

					// Stricter: flag ingredients that are missing unit or have non-positive qty
					if nm != "" && !op {
						if un == "" {
							problems = append(problems, fmt.Sprintf("recipe %q ingredient %q missing unit", id, nm))
						}
						if !(qv > 0) {
							problems = append(problems, fmt.Sprintf("recipe %q ingredient %q has non-positive qty", id, nm))
						}
					}

					needs = append(needs, need{name: nm, qty: qv, unit: un, optional: op})
				}
			}
		}
		recByID[id] = rec{baseServ: base, needs: needs}
	}

	// ---- accumulate total required quantities across the whole plan ----
	type req struct {
		qty  float64
		unit string
	}
	required := map[string]req{}       // ingredient name (lower) -> aggregate requirement
	conflicted := map[string]bool{}    // ingredient name -> unit conflict already reported
	unknownRecipe := map[string]bool{} // dedupe unknown id messages
	nonPosServ := map[string]bool{}    // dedupe non-positive servings messages

	for _, day := range plan.DaysPlanned {
		for _, m := range day.Meals {
			r, ok := recByID[m.ID]
			if !ok {
				if !unknownRecipe[m.ID] {
					problems = append(problems, fmt.Sprintf("unknown recipe id: %q", m.ID))
					unknownRecipe[m.ID] = true
				}
				continue
			}
			if m.Servings <= 0 {
				if !nonPosServ[m.ID] {
					problems = append(problems, fmt.Sprintf("meal %q has non-positive servings", m.ID))
					nonPosServ[m.ID] = true
				}
				continue
			}
			scale := float64(m.Servings) / float64(r.baseServ)
			for _, n := range r.needs {
				if n.name == "" || n.unit == "" || !(n.qty > 0) {
					// Skip malformed needs; already flagged above for non-optional.
					continue
				}
				if n.optional {
					continue // optionals don't block feasibility
				}
				q := n.qty * scale
				cur := required[n.name]
				if cur.unit != "" && cur.unit != n.unit {
					if !conflicted[n.name] {
						problems = append(problems, fmt.Sprintf("unit conflict for %q (%s vs %s)", n.name, cur.unit, n.unit))
						conflicted[n.name] = true
					}
					continue
				}
				cur.unit = n.unit
				cur.qty += q
				required[n.name] = cur
			}
		}
	}

	// ---- compare required vs pantry ----
	for name, reqv := range required {
		p, ok := pantryIdx[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("missing ingredient: %s (need %.4g %s)", name, reqv.qty, reqv.unit))
			continue
		}
		if p.unit != reqv.unit {
			problems = append(problems, fmt.Sprintf("unit mismatch: %s (need %s, have %s)", name, reqv.unit, p.unit))
			continue
		}
		if p.qty+eps < reqv.qty {
			problems = append(problems, fmt.Sprintf("insufficient %s (need %.4g %s, have %.4g %s)", name, reqv.qty, reqv.unit, p.qty, p.unit))
			continue
		}
	}

	if len(problems) > 0 {
		sort.Strings(problems)
		return false, problems, nil
	}
	return true, nil, nil
}

func (c *InstrumentedCoordinator) logIteration(iter pantryagent.IterationLog) {
	if c.logger != nil {
		if err := c.logger.LogIteration(iter); err != nil {
			slog.Error("Failed to log coordination iteration", "error", err, "iteration", iter.Iteration)
		}
	}
}
