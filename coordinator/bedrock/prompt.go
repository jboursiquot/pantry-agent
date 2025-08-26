package bedrock

import (
	"encoding/json"
	"pantryagent"
)

type Prompt struct {
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
}

func NewPrompt(task string, tp pantryagent.ToolProvider) (Prompt, error) {
	tools := tp.GetTools()

	bedrockTools := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		bedrockTools = append(bedrockTools, Tool{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		})
	}

	return Prompt{
		Messages: []Message{
			{
				Role: "system",
				Content: []MessagePart{
					{
						Type: "text",
						Text: string(systemPrompt),
					},
				},
			},
			{
				Role: "user",
				Content: []MessagePart{
					{
						Type: "text",
						Text: task,
					},
				},
			},
		},
		Tools: bedrockTools,
	}, nil
}

const systemPrompt = `You are a meal-planning coordinator.

GOAL:
Plan meals over the user-specified days and servings, using the tools to gather pantry state and available recipes, then return the final meal plan JSON.

FINAL OUTPUT FORMAT:
When you are ready to complete the task, return ONLY the JSON object - no explanations, no text before or after, no markdown formatting. Start immediately with { and end with }.

Example of correct final response format:
{
  "summary": "3-day dinner plan...",
  "days_planned": [...]
}

JSON Schema:
{
  "summary": string,                         // <= 400 chars: overview of the plan and prioritization of perishables
  "days_planned": [                          // MUST contain at least one element
    {
      "day": integer,                        // starting at 1
      "meals": [                             // 1..M meals for that day
        {
          "id": string,                      // recipe id
          "name": string,                    // recipe name
          "servings": integer                // servings for this meal
        }
      ]
    }
  ]
}

If any field has no content, use an empty array [] or "" appropriately.  
days_planned must NEVER be empty — always return at least one day.  
The JSON must be valid UTF-8, with no commentary, no markdown, and no trailing commas.  

TOOL USE:
When you need more information, use the provided tools directly through the tool interface.  
Do not wrap tool requests in JSON text such as {"tool_calls":[...]}.  
Do not echo tool results yourself — the coordinator will supply them.  

CRITICAL RULES:
- When returning the final meal plan, output ONLY the JSON object with no explanatory text before or after it
- Final output must be valid JSON only (no explanations or code fences)
- Never invent recipe IDs (only use from recipe_get).
- Never assume unit conversions; mismatched units are unusable.
- Always call pantry_get before finalizing.
- Always call recipe_get before selecting meals.
- Prioritize ingredients with the lowest days_left when choosing meals.
- The coordinator will check feasibility; your final plan must fit the pantry without shortages or unit mismatches.
- days_planned must always contain at least one element.
- Call pantry_get and recipe_get at most once each per session.
- Reuse the latest tool_result content already provided; do not re-call a tool unless the coordinator says the data changed.
- If you already have pantry + recipes, proceed to planning and produce the final JSON.
`

// HasToolResult returns true if a tool result for the specified tool name exists in the prompt's message history.
// It checks for a message with role "tool" whose first content part contains a JSON object
// with a "tool_result" field equal to the given tool name.
func (p *Prompt) HasToolResult(tool string) bool {
	for _, msg := range p.Messages {
		if msg.Role != "tool" || len(msg.Content) == 0 {
			continue
		}
		text := msg.Content[0].Text

		var payload struct {
			ToolResult string `json:"tool_result"`
		}
		if json.Unmarshal([]byte(text), &payload) == nil && payload.ToolResult == tool {
			return true
		}
	}
	return false
}

// HasToolResultInContent returns true if a tool result for the specified tool name exists in any message content.
// It checks all messages (regardless of role) for JSON objects with a "tool_result" field equal to the given tool name.
// This is useful for coordinators that embed tool results in user messages rather than using formal tool roles.
func (p *Prompt) HasToolResultInContent(tool string) bool {
	for _, msg := range p.Messages {
		for _, part := range msg.Content {
			if part.Type != "text" {
				continue
			}

			var payload struct {
				ToolResult string `json:"tool_result"`
			}
			if json.Unmarshal([]byte(part.Text), &payload) == nil && payload.ToolResult == tool {
				return true
			}
		}
	}
	return false
}
