package llm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
)

// Scripted is one answer a replay server will give. Answer is the JSON
// object the model "returns"; if it is a JSON string, that string is
// returned verbatim, which lets a script include malformed answers.
type Scripted struct {
	Purpose string          `json:"purpose"`
	Answer  json.RawMessage `json:"answer"`
}

// Replay is a fake OpenAI-compatible server that returns scripted answers
// in order, per purpose. It is used by tests and by replay-mode eval.
type Replay struct {
	*httptest.Server
	mu      sync.Mutex
	script  []Scripted
	used    []bool
	Bodies  [][]byte // every request body received, in order
	Missing []string // purposes requested with no scripted answer left
}

// NewReplay starts a replay server. Close it when done.
func NewReplay(script []Scripted) *Replay {
	r := &Replay{script: script, used: make([]bool, len(script))}
	r.Server = httptest.NewServer(http.HandlerFunc(r.serve))
	return r
}

func (r *Replay) serve(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	purpose := req.Header.Get(PurposeHeader)
	r.mu.Lock()
	r.Bodies = append(r.Bodies, body)
	idx := -1
	for i, s := range r.script {
		if !r.used[i] && s.Purpose == purpose {
			idx = i
			r.used[i] = true
			break
		}
	}
	if idx < 0 {
		r.Missing = append(r.Missing, purpose)
	}
	r.mu.Unlock()
	if idx < 0 {
		http.Error(w, fmt.Sprintf("no scripted answer left for purpose %q", purpose), http.StatusInternalServerError)
		return
	}
	content := string(r.script[idx].Answer)
	var s string
	if json.Unmarshal(r.script[idx].Answer, &s) == nil {
		content = s
	}
	resp := map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: content}}}}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// Unused returns the purposes of scripted answers never requested.
func (r *Replay) Unused() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for i, u := range r.used {
		if !u {
			out = append(out, r.script[i].Purpose)
		}
	}
	return out
}
