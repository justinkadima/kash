// Package ai implements a streaming client for OpenAI-compatible chat
// completion endpoints (Ollama's /v1 API, llama.cpp server, LM Studio,
// vLLM, OpenRouter, ...).
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Message is a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client talks to one OpenAI-compatible server.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// New builds a client. Streams can be slow, so there is no overall request
// timeout; cancellation is the caller's job via context.
func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 0},
	}
}

// Delta carries one streaming event: a text chunk, a terminal error, or
// the final marker (Final, with Err nil on success).
type Delta struct {
	Text  string
	Err   error
	Final bool
}

// Stream starts a chat completion and returns a channel of deltas. The
// channel is closed by the reader goroutine after the final event.
func (c *Client) Stream(ctx context.Context, model string, temperature float64, msgs []Message) (<-chan Delta, error) {
	body, err := json.Marshal(map[string]any{
		"model":       model,
		"messages":    msgs,
		"temperature": temperature,
		"stream":      true,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		msg := readErrBody(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("api: %s: %s", resp.Status, msg)
	}

	ch := make(chan Delta, 16)
	go readSSE(resp.Body, ch)
	return ch, nil
}

// readErrBody extracts a human-readable error message from an API error
// response, tolerating arbitrary payloads.
func readErrBody(r io.Reader) string {
	data, err := io.ReadAll(r)
	if err != nil || len(data) == 0 {
		return "request failed"
	}
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// readSSE parses a server-sent-events stream into Delta events.
func readSSE(r io.ReadCloser, ch chan<- Delta) {
	defer close(ch)
	defer r.Close()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if !strings.HasPrefix(line, "data:") {
			continue // comment, event id, keepalive, etc.
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			ch <- Delta{Final: true}
			return
		}
		var payload struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			continue // tolerate junk lines (usage chunks, pings)
		}
		if len(payload.Choices) > 0 && payload.Choices[0].Delta.Content != "" {
			ch <- Delta{Text: payload.Choices[0].Delta.Content}
		}
	}
	if err := sc.Err(); err != nil {
		ch <- Delta{Err: fmt.Errorf("stream: %w", err), Final: true}
		return
	}
	// Server hung up without [DONE]; treat as completion.
	ch <- Delta{Final: true}
}

// Models fetches the list of model ids from /v1/models.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("api: %s: %s", resp.Status, readErrBody(io.LimitReader(resp.Body, 4096)))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
