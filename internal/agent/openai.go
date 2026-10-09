package agent

import (
	"context"
	"encoding/json"
	"net/http"
)

type openai struct {
	cfg ProviderConfig
	c   *http.Client
	url string
	key string
}

func (o *openai) Name() string { return "openai-compatible:" + o.cfg.Model }

func (o *openai) Complete(ctx context.Context, req Request) (Response, error) {
	msgs := []map[string]any{{"role": "system", "content": req.System}}
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			msgs = append(msgs, map[string]any{"role": "user", "content": m.Content})
		case "assistant":
			msg := map[string]any{"role": "assistant", "content": m.Content}
			var calls []map[string]any
			for _, tc := range m.ToolCalls {
				args := string(tc.Args)
				if args == "" {
					args = "{}"
				}
				calls = append(calls, map[string]any{"id": tc.ID, "type": "function", "function": map[string]any{"name": tc.Name, "arguments": args}})
			}
			if len(calls) > 0 {
				msg["tool_calls"] = calls
			}
			msgs = append(msgs, msg)
		case "tool":
			msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": m.ToolCallID, "content": m.Content})
		}
	}
	body := map[string]any{"model": o.cfg.Model, "max_tokens": req.MaxTokens, "messages": msgs}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Schema}})
		}
		body["tools"] = tools
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			In  int `json:"prompt_tokens"`
			Out int `json:"completion_tokens"`
		} `json:"usage"`
	}
	headers := map[string]string{}
	if o.key != "" {
		headers["Authorization"] = "Bearer " + o.key
	}
	if err := doJSON(ctx, o.c, http.MethodPost, o.url, headers, body, &out); err != nil {
		return Response{}, err
	}
	r := Response{InputTokens: out.Usage.In, OutputTokens: out.Usage.Out}
	if len(out.Choices) > 0 {
		m := out.Choices[0].Message
		r.Text = m.Content
		for _, tc := range m.ToolCalls {
			r.ToolCalls = append(r.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Args: json.RawMessage(tc.Function.Arguments)})
		}
	}
	return r, nil
}
