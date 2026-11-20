// Package llm is a small client for the OpenAI-compatible chat completions
// API. Every answer is parsed into a Go struct that rejects unknown and
// missing fields, with one retry. In test mode a tripwire scans every
// outgoing request for canary strings and refuses to send it if one is
// present, and a copy of every request that is sent is saved to disk.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrTripwire means a canary string was about to leave the process.
var ErrTripwire = errors.New("tripwire: canary found in outgoing model request")

// PurposeHeader tells a replay server which scripted answer to return.
const PurposeHeader = "X-Hody-Purpose"

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Config configures a Client. Canaries switch on test mode.
type Config struct {
	BaseURL   string
	APIKey    string
	Model     string
	Canaries  []string
	RecordDir string // required in test mode
	HTTP      *http.Client
}

// Client talks to one model endpoint. It is not safe for concurrent use;
// each diagnosis gets its own.
type Client struct {
	cfg Config
	seq int
}

// New validates the configuration.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("HODY_LLM_BASE_URL must be an http(s) URL, got %q", cfg.BaseURL)
	}
	if cfg.Model == "" {
		return nil, errors.New("HODY_LLM_MODEL is not set")
	}
	if len(cfg.Canaries) > 0 && cfg.RecordDir == "" {
		return nil, errors.New("test mode (canaries set) needs a record directory")
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 180 * time.Second}
	}
	return &Client{cfg: cfg}, nil
}

type chatRequest struct {
	Model          string            `json:"model"`
	Messages       []Message         `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// Complete sends one request and returns the text of the first choice.
func (c *Client) Complete(ctx context.Context, purpose string, msgs []Message) (string, error) {
	body, err := json.Marshal(chatRequest{Model: c.cfg.Model, Messages: msgs, ResponseFormat: map[string]string{"type": "json_object"}})
	if err != nil {
		return "", err
	}
	if err := c.tripwire(body); err != nil {
		return "", fmt.Errorf("%s request: %w", purpose, err)
	}
	c.seq++
	if err := c.record(purpose, body); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(PurposeHeader, purpose)
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.cfg.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("model request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model returned HTTP %d: %.200s", resp.StatusCode, data)
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil || len(cr.Choices) == 0 {
		return "", fmt.Errorf("model response is not a chat completion: %.200s", data)
	}
	return cr.Choices[0].Message.Content, nil
}

// tripwire fails if any canary appears in body, raw or JSON-escaped.
func (c *Client) tripwire(body []byte) error {
	for i, canary := range c.cfg.Canaries {
		if canary == "" {
			continue
		}
		esc, _ := json.Marshal(canary)
		if bytes.Contains(body, []byte(canary)) || bytes.Contains(body, esc[1:len(esc)-1]) {
			return fmt.Errorf("%w (canary #%d)", ErrTripwire, i+1)
		}
	}
	return nil
}

// record saves a copy of an outgoing request in test mode.
func (c *Client) record(purpose string, body []byte) error {
	if c.cfg.RecordDir == "" {
		return nil
	}
	if err := os.MkdirAll(c.cfg.RecordDir, 0o700); err != nil {
		return err
	}
	name := filepath.Join(c.cfg.RecordDir, fmt.Sprintf("%03d-%s.json", c.seq, purpose))
	return os.WriteFile(name, body, 0o600)
}

// Ask sends msgs and parses the answer into out, retrying once with the
// parse error if the first answer is invalid.
func (c *Client) Ask(ctx context.Context, purpose string, msgs []Message, out any) error {
	text, err := c.Complete(ctx, purpose, msgs)
	if err != nil {
		return err
	}
	perr := Parse(text, out)
	if perr == nil {
		return nil
	}
	retry := append(append([]Message(nil), msgs...),
		Message{Role: "assistant", Content: text},
		Message{Role: "user", Content: "Your answer was rejected: " + perr.Error() + ". Reply again with only the JSON object described, with exactly the required fields."})
	text, err = c.Complete(ctx, purpose, retry)
	if err != nil {
		return err
	}
	if err := Parse(text, out); err != nil {
		return fmt.Errorf("%s: model answer invalid after retry: %w", purpose, err)
	}
	return nil
}
