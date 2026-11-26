package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type answer struct {
	Verdict string   `json:"verdict"`
	Reason  string   `json:"reason"`
	Extra   []string `json:"extra,omitempty"`
}

func (a *answer) Validate() error {
	if a.Verdict != "approve" && a.Verdict != "block" {
		return errors.New("verdict must be approve or block")
	}
	return nil
}

func TestParse(t *testing.T) {
	cases := []struct {
		name, in string
		ok       bool
	}{
		{"valid", `{"verdict":"approve","reason":"fine"}`, true},
		{"optional field omitted", `{"verdict":"block","reason":"x"}`, true},
		{"code fence", "```json\n{\"verdict\":\"approve\",\"reason\":\"r\"}\n```", true},
		{"unknown field", `{"verdict":"approve","reason":"r","url":"http://x"}`, false},
		{"missing field", `{"verdict":"approve"}`, false},
		{"wrong type", `{"verdict":1,"reason":"r"}`, false},
		{"fails Validate", `{"verdict":"maybe","reason":"r"}`, false},
		{"not json", `the verdict is approve`, false},
		{"trailing data", `{"verdict":"approve","reason":"r"} {"x":1}`, false},
		{"array", `[{"verdict":"approve","reason":"r"}]`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var a answer
			err := Parse(c.in, &a)
			if (err == nil) != c.ok {
				t.Fatalf("Parse ok=%v, err=%v", c.ok, err)
			}
		})
	}
}

func script(t *testing.T, items ...string) []Scripted {
	t.Helper()
	var s []Scripted
	for i := 0; i < len(items); i += 2 {
		s = append(s, Scripted{Purpose: items[i], Answer: json.RawMessage(items[i+1])})
	}
	return s
}

func newClient(t *testing.T, r *Replay, canaries ...string) (*Client, string) {
	t.Helper()
	dir := t.TempDir()
	c, err := New(Config{BaseURL: r.URL, Model: "m", Canaries: canaries, RecordDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return c, dir
}

func TestAskRetriesOnceThenSucceeds(t *testing.T) {
	r := NewReplay(script(t,
		"critic", `{"verdict":"approve","reason":"r","sneaky":true}`,
		"critic", `{"verdict":"approve","reason":"r"}`))
	defer r.Close()
	c, _ := newClient(t, r)
	var a answer
	if err := c.Ask(context.Background(), "critic", []Message{{Role: "user", Content: "q"}}, &a); err != nil {
		t.Fatal(err)
	}
	if len(r.Bodies) != 2 || !strings.Contains(string(r.Bodies[1]), "rejected") {
		t.Fatalf("expected a retry carrying the rejection, got %d requests", len(r.Bodies))
	}
}

func TestAskFailsAfterSecondInvalidAnswer(t *testing.T) {
	r := NewReplay(script(t, "critic", `"not json"`, "critic", `{"verdict":"approve"}`))
	defer r.Close()
	c, _ := newClient(t, r)
	var a answer
	if err := c.Ask(context.Background(), "critic", nil, &a); err == nil {
		t.Fatal("expected failure after retry")
	}
	if len(r.Bodies) != 2 {
		t.Fatalf("expected exactly one retry, got %d requests", len(r.Bodies))
	}
}

func TestTripwireBlocksCanary(t *testing.T) {
	cases := []struct{ name, canary, content string }{
		{"plain", "CANARY-abc123", "log line CANARY-abc123 here"},
		{"needs JSON escaping", `CAN"ARY<x>`, `value CAN"ARY<x> end`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := NewReplay(script(t, "triage", `{"verdict":"approve","reason":"r"}`))
			defer r.Close()
			cl, dir := newClient(t, r, c.canary)
			_, err := cl.Complete(context.Background(), "triage", []Message{{Role: "user", Content: c.content}})
			if !errors.Is(err, ErrTripwire) {
				t.Fatalf("expected tripwire, got %v", err)
			}
			if len(r.Bodies) != 0 {
				t.Fatal("request reached the server")
			}
			if files, _ := os.ReadDir(dir); len(files) != 0 {
				t.Fatal("blocked request must not be saved")
			}
		})
	}
}

func TestTestModeRecordsRequests(t *testing.T) {
	r := NewReplay(script(t, "conclude", `{"verdict":"approve","reason":"r"}`))
	defer r.Close()
	c, dir := newClient(t, r, "CANARY-unused")
	if _, err := c.Complete(context.Background(), "conclude", []Message{{Role: "user", Content: "hello"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "001-conclude.json"))
	if err != nil || !strings.Contains(string(data), "hello") {
		t.Fatalf("recorded request missing: %v", err)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		ok   bool
	}{
		{"ok", Config{BaseURL: "http://localhost:11434/v1", Model: "m"}, true},
		{"bad scheme", Config{BaseURL: "file:///etc", Model: "m"}, false},
		{"no model", Config{BaseURL: "http://x"}, false},
		{"canaries without record dir", Config{BaseURL: "http://x", Model: "m", Canaries: []string{"c"}}, false},
	}
	for _, c := range cases {
		if _, err := New(c.cfg); (err == nil) != c.ok {
			t.Errorf("%s: err=%v", c.name, err)
		}
	}
}

func TestReplayReportsMissingAnswer(t *testing.T) {
	r := NewReplay(nil)
	defer r.Close()
	c, _ := newClient(t, r)
	if _, err := c.Complete(context.Background(), "fix", nil); err == nil {
		t.Fatal("expected error")
	}
	if len(r.Missing) != 1 || r.Missing[0] != "fix" {
		t.Fatalf("missing=%v", r.Missing)
	}
}

func TestTimeout(t *testing.T) {
	c, err := New(Config{BaseURL: "http://127.0.0.1:1", Model: "m"})
	if err != nil || c.cfg.HTTP.Timeout != DefaultTimeout {
		t.Fatalf("default timeout: err=%v", err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	c, _ = New(Config{BaseURL: slow.URL, Model: "m", Timeout: 50 * time.Millisecond})
	start := time.Now()
	_, err = c.Complete(context.Background(), "conclude", []Message{{Role: "user", Content: "x"}})
	if err == nil || time.Since(start) > 900*time.Millisecond {
		t.Fatalf("slow model: err=%v after %s, want a timeout after about 50ms", err, time.Since(start))
	}
}
