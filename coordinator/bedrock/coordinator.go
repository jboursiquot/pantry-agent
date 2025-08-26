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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
)

// Coordinator is responsible for managing the interaction between the LLM, tools, and output channel.
type Coordinator struct {
	llm            llmClient
	toolProvider   pantryagent.ToolProvider
	maxIterations  int
	logger         pantryagent.CoordinationLogger
	pantry         map[string]any
	recipes        []any
	tracerProvider *trace.TracerProvider
}

type llmClient interface {
	Invoke(ctx context.Context, prompt Prompt) (Response, error)
}

// NewCoordinator initializes a new coordinator.
func NewCoordinator(llm llmClient, toolRegistry pantryagent.ToolProvider, pantryData map[string]any, recipeData []any, maxIterations int, logger pantryagent.CoordinationLogger, tracerProvider *trace.TracerProvider) *Coordinator {
	return &Coordinator{
		llm:            llm,
		toolProvider:   toolRegistry,
		maxIterations:  maxIterations,
		logger:         logger,
		pantry:         pantryData,
		recipes:        recipeData,
		tracerProvider: tracerProvider,
	}
}

// Run executes the coordination process for a given task.
func (c *Coordinator) Run(ctx context.Context, task string) (string, error) {
	ctx, span := otel.Tracer(pantryagent.TracerNameBedrock).Start(ctx, "Coordinator.Run")
	defer span.End()

	slog.Info("COORDINATOR: Starting run", "task", task)

	prompt, err := NewPrompt(task, c.toolProvider)
	if err != nil {
		return "", fmt.Errorf("failed to apply system prompt: %w", err)
	}

	var finalOut string
	toolsAlreadyCalled := make(map[string]int) // Track how many times each tool has been called

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
				}(),
			)
		}

		// 1) Invoke model
		res, err := c.llm.Invoke(ctx, prompt)
		if err != nil {
			iterLog.Error = err.Error()
			c.logIteration(iterLog)
			return "", fmt.Errorf("invoke failed: %w", err)
		}
		iterLog.LLMOutput = res

		slog.Info("COORDINATOR: LLM response received",
			"iteration", iter+1,
			"content_length", len(res.Content),
			"tool_calls", len(res.ToolCalls),
		)

		// If the assistant returned no tool calls, treat content as a potential final plan.
		if len(res.ToolCalls) == 0 {
			slog.Info("COORDINATOR: No tool calls; attempting to treat output as final plan", "iteration", iter+1, "content_length", len(res.Content))
			finalJSON := strings.TrimSpace(res.Content)

			// Validate final JSON structure.
			if finalJSON == "" || !strings.HasPrefix(finalJSON, "{") || !strings.HasSuffix(finalJSON, "}") {
				slog.Info("COORDINATOR: Output is not valid JSON format", "iteration", iter+1, "starts_with_brace", strings.HasPrefix(finalJSON, "{"), "ends_with_brace", strings.HasSuffix(finalJSON, "}"))
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

			feasible, probs, ferr := c.checkFeasible(finalJSON)
			if ferr != nil {
				slog.Error("COORDINATOR: Feasibility check failed", "error", ferr, "iteration", iter+1)
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

			if !feasible {
				// Tell the model exactly why and ask it to re-plan (no mutations in this project).
				slog.Warn("COORDINATOR: Feasibility check failed", "iteration", iter+1, "problems", probs)
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
			finalOut = finalJSON
			c.logIteration(iterLog)
			break
		}

		// Model requested tool calls: check for excessive repetition first
		var hasExcessiveRepetition bool
		for _, call := range res.ToolCalls {
			toolsAlreadyCalled[call.Name]++

			// Detect excessive repetition of data-gathering tools
			if (call.Name == "pantry_get" || call.Name == "recipe_get") && toolsAlreadyCalled[call.Name] > 2 {
				slog.Warn("COORDINATOR: Excessive tool repetition detected", "tool", call.Name, "count", toolsAlreadyCalled[call.Name], "iteration", iter+1)
				hasExcessiveRepetition = true
				break
			}
		}

		if hasExcessiveRepetition {
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
			tlog := pantryagent.ToolCallLog{Name: call.Name, Input: call.Input}
			tool, gerr := c.toolProvider.GetTool(call.Name)
			if gerr != nil {
				tlog.Error = gerr.Error()
				toolCallLogs = append(toolCallLogs, tlog)
				toolResults = append(toolResults, ToolResult{
					ToolUseID: call.ToolUseID,
					ToolName:  call.Name,
					Data:      map[string]any{"error": fmt.Sprintf("tool %q not found: %v", call.Name, gerr)},
				})
				continue
			}

			result, rerr := tool.Run(ctx, call.Input)
			if rerr != nil {
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
		}

		if len(toolResults) > 0 {
			prompt.Messages = append(prompt.Messages, NewToolResultMessage(toolResults))
		}

		iterLog.ToolCalls = toolCallLogs
		c.logIteration(iterLog)
	}

	return finalOut, nil
}

// checkFeasible validates that a candidate final JSON meal plan is doable with the
// most recent pantry and recipe catalog (no unit conversions, no shortages).
func (c *Coordinator) checkFeasible(finalJSON string) (ok bool, problems []string, err error) {
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

func (c *Coordinator) logIteration(iter pantryagent.IterationLog) {
	if c.logger != nil {
		if err := c.logger.LogIteration(iter); err != nil {
			slog.Error("Failed to log coordination iteration", "error", err, "iteration", iter.Iteration)
		}
	}
}
