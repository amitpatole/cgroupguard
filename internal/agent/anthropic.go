package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type anthropic struct {
	cfg ProviderConfig
	c   *http.Client
	url string
	key string
}

// NewLLM builds a client from the configuration. It fails if the key variable is unset.
func NewLLM(cfg ProviderConfig) (LLM, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("--model is required")
	}
	key := os.Getenv(cfg.APIKeyEnv)
	switch cfg.Kind {
	case "anthropic":
		base := cfg.BaseURL
		if base == "" {
			base = "https://api.anthropic.com"
		}
		u, err := checkURL(base, cfg.AllowInsecure)
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, fmt.Errorf("environment variable %q is empty", cfg.APIKeyEnv)
		}
		return &anthropic{cfg: cfg, c: httpClient(), url: strings.TrimRight(u.String(), "/") + "/v1/messages", key: key}, nil
	case "openai-compatible":
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("--base-url is required for openai-compatible providers (for example http://localhost:11434/v1 for Ollama)")
		}
		u, err := checkURL(cfg.BaseURL, cfg.AllowInsecure)
		if err != nil {
			return nil, err
		}
		// Local servers such as Ollama need no key; hosted ones do.
		return &openai{cfg: cfg, c: httpClient(), url: strings.TrimRight(u.String(), "/") + "/chat/completions", key: key}, nil
	}
	return nil, fmt.Errorf("unknown provider %q (use anthropic or openai-compatible)", cfg.Kind)
}

func (a *anthropic) Name() string { return "anthropic:" + a.cfg.Model }

func (a *anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	type block map[string]any
	var msgs []map[string]any
	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			msgs = append(msgs, map[string]any{"role": "user", "content": m.Content})
		case "assistant":
			var blocks []block
			if m.Content != "" {
				blocks = append(blocks, block{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input any = map[string]any{}
				if len(tc.Args) > 0 {
					_ = json.Unmarshal(tc.Args, &input)
				}
				blocks = append(blocks, block{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": input})
			}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": blocks})
		case "tool":
			msgs = append(msgs, map[string]any{"role": "user", "content": []block{{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content}}})
		}
	}
	body := map[string]any{"model": a.cfg.Model, "max_tokens": req.MaxTokens, "system": req.System, "messages": msgs}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Schema})
		}
		body["tools"] = tools
	}
	var out struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			In  int `json:"input_tokens"`
			Out int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := doJSON(ctx, a.c, http.MethodPost, a.url, map[string]string{"x-api-key": a.key, "anthropic-version": "2023-06-01"}, body, &out); err != nil {
		return Response{}, err
	}
	r := Response{InputTokens: out.Usage.In, OutputTokens: out.Usage.Out}
	for _, c := range out.Content {
		switch c.Type {
		case "text":
			r.Text += c.Text
		case "tool_use":
			r.ToolCalls = append(r.ToolCalls, ToolCall{ID: c.ID, Name: c.Name, Args: c.Input})
		}
	}
	return r, nil
}
