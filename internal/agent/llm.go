// Package agent is an optional, read-only assistant layer. The deterministic rules
// engine and planner never depend on it. A model can add commentary and
// suggestions; it cannot change a plan, run a command or reach the cluster.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Message is one turn. Tool results are sent with Role "tool".
type Message struct {
	Role       string     `json:"role"` // user | assistant | tool
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string     `json:"toolCallId,omitempty"`
}

type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
}

type Request struct {
	System    string
	Messages  []Message
	Tools     []ToolSpec
	MaxTokens int
}

type Response struct {
	Text         string
	ToolCalls    []ToolCall
	InputTokens  int
	OutputTokens int
}

// LLM is the provider-agnostic interface. Implementations must not log request bodies
// or credentials.
type LLM interface {
	Complete(ctx context.Context, req Request) (Response, error)
	Name() string
}

// ProviderConfig describes how to reach a model. The API key is read from the
// environment variable named by APIKeyEnv, never from a flag or file.
type ProviderConfig struct {
	Kind          string // anthropic | openai-compatible
	Model         string
	BaseURL       string
	APIKeyEnv     string
	AllowInsecure bool // permit plain http to a non-loopback host
}

const maxResponseBytes = 4 << 20

var ErrProvider = errors.New("model provider error")

func httpClient() *http.Client {
	// No redirects: a configured endpoint must not bounce credentials to another host.
	return &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// checkURL rejects URLs that could leak credentials or data: userinfo, non-http(s)
// schemes, and plain http to anything but loopback unless explicitly allowed.
func checkURL(raw string, allowInsecure bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid base URL: use http(s)://host[:port] without credentials")
	}
	if u.Scheme == "http" && !allowInsecure {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("refusing plain http to %s: use https or pass --allow-insecure-http", host)
		}
	}
	return u, nil
}

func doJSON(ctx context.Context, c *http.Client, method, endpoint string, headers map[string]string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("%w: request failed: %s", ErrProvider, scrubErr(err))
	}
	defer resp.Body.Close()
	var buf strings.Builder
	n, _ := bufCopy(&buf, resp.Body, maxResponseBytes+1)
	if n > maxResponseBytes {
		return fmt.Errorf("%w: response exceeds %d bytes", ErrProvider, maxResponseBytes)
	}
	if resp.StatusCode/100 != 2 {
		// The provider's error text is not echoed: it can contain request fragments.
		return fmt.Errorf("%w: HTTP %d", ErrProvider, resp.StatusCode)
	}
	if err := json.Unmarshal([]byte(buf.String()), out); err != nil {
		return fmt.Errorf("%w: unreadable response", ErrProvider)
	}
	return nil
}

// scrubErr drops the URL from transport errors so a key in a query string cannot leak.
func scrubErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}
