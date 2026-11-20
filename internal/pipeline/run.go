// Package pipeline runs a diagnosis as fixed steps in a fixed order:
// collect, triage, investigate, conclude, propose fix, review fix, approve.
// Each step is a function with the same signature taking the session.
// The pipeline reaches tools only through internal/tools, which returns
// gateway evidence; it never imports the Docker or Kubernetes clients.
package pipeline

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"hody/internal/gateway"
	"hody/internal/llm"
	"hody/internal/model"
	"hody/internal/store"
	"hody/internal/tools"
)

// MaxModelRequests is the investigate step's tool request budget.
const MaxModelRequests = 8

// Deps are what one run needs. Gateway must be the one the Tools registry
// uses, so placeholders stay consistent within the session.
type Deps struct {
	Tools   *tools.Registry
	LLM     *llm.Client
	Gateway *gateway.Gateway
	Store   *store.Store             // optional
	Notify  func(snap model.Session) // optional; called after every step
}

// Engine runs one diagnosis.
type Engine struct{ d Deps }

// New returns an engine for one session.
func New(d Deps) *Engine { return &Engine{d: d} }

// Step is the shared signature of every pipeline step.
type Step func(ctx context.Context, s *model.Session) error

type namedStep struct {
	name string
	run  Step
}

// Steps returns the steps in their fixed order.
func (e *Engine) Steps() []string {
	var names []string
	for _, st := range e.steps() {
		names = append(names, st.name)
	}
	return names
}

func (e *Engine) steps() []namedStep {
	return []namedStep{
		{"collect", e.collect},
		{"triage", e.triage},
		{"investigate", e.investigate},
		{"conclude", e.conclude},
		{"propose_fix", e.proposeFix},
		{"review_fix", e.reviewFix},
		{"approve", e.approve},
	}
}

// NewSession creates a session for a target.
func NewSession(t model.Target) *model.Session {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("pipeline: no randomness: " + err.Error())
	}
	return &model.Session{ID: hex.EncodeToString(b), Target: t, Created: time.Now().UTC(), Status: model.StatusRunning}
}

// Run executes every step in order and stops at the first error, which
// marks the session failed.
func (e *Engine) Run(ctx context.Context, s *model.Session) error {
	e.audit(s, "session_started", map[string]string{"target": s.Target.String()})
	for _, st := range e.steps() {
		s.Step = st.name
		if e.d.Notify != nil {
			e.d.Notify(s.Clone())
		}
		e.audit(s, "step", map[string]string{"step": st.name})
		if err := st.run(ctx, s); err != nil {
			s.Status = model.StatusFailed
			s.Error = fmt.Sprintf("%s: %v", st.name, err)
			e.audit(s, "failed", map[string]string{"step": st.name, "error": s.Error})
			e.save(s)
			return fmt.Errorf("%s: %w", st.name, err)
		}
		e.save(s)
	}
	return nil
}

func (e *Engine) save(s *model.Session) {
	if e.d.Notify != nil {
		e.d.Notify(s.Clone())
	}
	if e.d.Store != nil {
		if err := e.d.Store.Save(s); err != nil {
			s.Warnings = append(s.Warnings, "could not save session: "+err.Error())
		}
	}
}

func (e *Engine) audit(s *model.Session, event string, detail map[string]string) {
	if e.d.Store != nil {
		_ = e.d.Store.Audit(s.ID, event, detail)
	}
}

// call runs one tool request and records it on the session.
func (e *Engine) call(ctx context.Context, s *model.Session, by, tool string, args map[string]string) error {
	ev, err := e.d.Tools.Call(ctx, tool, args)
	tc := model.ToolCall{Tool: tool, Args: args, By: by, Accepted: err == nil}
	if err != nil {
		tc.Reason = err.Error()
	} else {
		tc.EvidenceID = ev.ID
		s.Evidence = append(s.Evidence, ev)
	}
	s.ToolCalls = append(s.ToolCalls, tc)
	e.audit(s, "tool_call", map[string]string{"tool": tool, "args": fmt.Sprint(args), "by": by,
		"accepted": fmt.Sprint(tc.Accepted), "reason": tc.Reason, "evidence": tc.EvidenceID})
	return err
}

// ask makes one typed model call and audits its purpose and result.
func (e *Engine) ask(ctx context.Context, s *model.Session, purpose string, msgs []llm.Message, out any) error {
	err := e.d.LLM.Ask(ctx, purpose, msgs, out)
	result := "ok"
	if err != nil {
		result = err.Error()
	}
	e.audit(s, "model_call", map[string]string{"purpose": purpose, "result": result})
	return err
}
