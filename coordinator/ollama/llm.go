package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"pantryagent"
)

type options struct {
	Temperature   float64 `json:"temperature,omitempty"`
	TopP          float64 `json:"top_p,omitempty"`
	RepeatPenalty float64 `json:"repeat_penalty,omitempty"`
	NumCtx        int     `json:"num_ctx,omitempty"`
}

type Client struct {
	endpoint     string
	model        string
	systemPrompt string
	httpClient   pantryagent.HTTPClient
	options      options
}

type ClientOpts struct {
	BaseEndpoint string
	ModelID      string
	Prompt       Prompt
	HTTPClient   pantryagent.HTTPClient
}

func NewClient(opts ClientOpts) (*Client, error) {
	if len(opts.Prompt.Messages) == 0 {
		return nil, fmt.Errorf("invalid system prompt")
	}

	return &Client{
		model:        opts.ModelID,
		systemPrompt: opts.Prompt.Messages[0].Content, // Use the first message content directly
		httpClient:   opts.HTTPClient,
		endpoint:     opts.BaseEndpoint + "/api/chat",
		options: options{
			Temperature:   0.2,
			TopP:          0.9,
			RepeatPenalty: 1.05,
			NumCtx:        16384, // instructor note: 16384 used as a safe default; raise if your machine can handle it
		},
	}, nil
}

type wireToolCall struct {
	Function struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"function"`
}

type wireMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	Name      string         `json:"name,omitempty"`
	ToolCalls []wireToolCall `json:"tool_calls,omitempty"`
}

type wireResponse struct {
	Message wireMessage `json:"message"`
	// other metadata omitted but available
}

type wireRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
	Options  options   `json:"options,omitempty"`
}

// Invoke sends the prompt to the Ollama API and returns the raw JSON string in Response.Content.
// Parsing decisions (tool_calls vs final) is delegated to the Coordinator.
func (c *Client) Invoke(ctx context.Context, prompt Prompt) (Response, error) {
	slog.Info("LLM_CLIENT: Invoked", "messages_len", len(prompt.Messages))

	msgs, err := c.buildRequest(prompt)
	if err != nil {
		return Response{}, err
	}

	reqBody := wireRequest{
		Model:    c.model,
		Messages: msgs,
		Tools:    prompt.Tools,
		Stream:   false,
		Options:  c.options,
	}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewBuffer(reqBytes))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("LLM_CLIENT: %s: %s", resp.Status, string(body))
	}

	var wr wireResponse
	if err := json.Unmarshal(body, &wr); err != nil {
		slog.Warn("LLM_CLIENT: decode failed, returning raw", "err", err, "body", string(body))
		return Response{Content: string(body)}, nil
	}

	if len(wr.Message.ToolCalls) > 0 {
		tc := make([]ToolCall, 0, len(wr.Message.ToolCalls))
		for _, call := range wr.Message.ToolCalls {
			tc = append(tc, ToolCall{
				Name: call.Function.Name,
				Args: call.Function.Arguments,
			})
		}
		return Response{Content: wr.Message.Content, ToolCalls: tc}, nil
	}

	// Return the model’s content verbatim; Likely the final response.
	return Response{Content: wr.Message.Content}, nil
}

// buildRequest converts the high-level Prompt into Ollama chat messages.
// - Prepends the client's systemPrompt (if non-empty)
// - Preserves user / assistant / tool roles (tool requires Name)
func (c *Client) buildRequest(prompt Prompt) ([]Message, error) {
	messages := make([]Message, 0, len(prompt.Messages)+1)

	// 1) System prompt (client-level)
	if sp := strings.TrimSpace(c.systemPrompt); sp != "" {
		messages = append(messages, Message{
			Role:    "system",
			Content: sp,
		})
	}

	// 2) Conversation history
	for _, m := range prompt.Messages {
		switch m.Role {
		case "system":
			// Skip user-inserted system blocks; we rely on c.systemPrompt.
			continue

		case "user", "assistant":
			messages = append(messages, Message{
				Role:    m.Role,
				Content: m.Content,
			})

		case "tool":
			// Native Ollama tool result: role=tool, name=<function>, content=<JSON string>
			if strings.TrimSpace(m.Name) == "" {
				slog.Warn("ollama: dropping tool message without name")
				continue
			}
			messages = append(messages, Message{
				Role:    "tool",
				Name:    m.Name,
				Content: m.Content,
			})

		default:
			slog.Warn("ollama: unknown role, coercing to user", "role", m.Role)
			messages = append(messages, Message{
				Role:    "user",
				Content: m.Content,
			})
		}
	}

	return messages, nil
}
