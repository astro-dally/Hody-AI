// Package model holds the data types shared by every part of Hody: the
// diagnosis session and the evidence, hypotheses, root cause, fix and review
// it accumulates. The types hold only redacted text; nothing here may carry
// a raw tool output or a secret value.
package model

import "time"

// Platforms Hody can diagnose.
const (
	PlatformDocker = "docker"
	PlatformK8s    = "k8s"
)

// Outcomes of the conclude step.
const (
	OutcomeRootCause    = "root_cause"
	OutcomeInsufficient = "insufficient_evidence"
)

// Verdicts of the fix review, ordered from least to most strict.
const (
	VerdictApprove = "approve"
	VerdictWarn    = "warn"
	VerdictBlock   = "block"
)

// Session statuses.
const (
	StatusRunning  = "running"
	StatusComplete = "complete"
	StatusFailed   = "failed"
)

// Target names the one object a diagnosis is about. It is also the scope:
// tools may only look at this container, or at this pod's namespace.
type Target struct {
	Platform  string `json:"platform"`
	Container string `json:"container,omitempty"` // docker
	Namespace string `json:"namespace,omitempty"` // k8s
	Pod       string `json:"pod,omitempty"`       // k8s
	File      string `json:"file,omitempty"`      // optional local manifest to fix
}

// String renders the target the way the CLI accepts it.
func (t Target) String() string {
	if t.Platform == PlatformDocker {
		return "docker " + t.Container
	}
	return "k8s pod/" + t.Pod + " -n " + t.Namespace
}

// Flag marks one evidence line that looks like an instruction aimed at an AI.
type Flag struct {
	Line int    `json:"line"`
	Rule string `json:"rule"`
	Text string `json:"text"`
}

// Evidence is one tool result after it passed through the gateway.
type Evidence struct {
	ID         string            `json:"id"`
	Tool       string            `json:"tool"`
	Args       map[string]string `json:"args"`
	Time       time.Time         `json:"time"`
	RawSHA256  string            `json:"raw_sha256"`
	Text       string            `json:"text"`  // redacted and limited
	Block      string            `json:"block"` // Text wrapped in data delimiters
	Flags      []Flag            `json:"flags,omitempty"`
	Redactions int               `json:"redactions"`
	Omitted    int               `json:"omitted_lines"`
}

// Hypothesis is a starting idea from triage.
type Hypothesis struct {
	Source      string   `json:"source"` // "rule:<name>" or "model"
	Category    string   `json:"category"`
	Summary     string   `json:"summary"`
	EvidenceIDs []string `json:"evidence_ids"`
}

// ToolCall records one tool request made during the session.
type ToolCall struct {
	Tool       string            `json:"tool"`
	Args       map[string]string `json:"args"`
	By         string            `json:"by"` // "collect" or "model"
	Accepted   bool              `json:"accepted"`
	Reason     string            `json:"reason,omitempty"`
	EvidenceID string            `json:"evidence_id,omitempty"`
}

// RootCause is the conclusion, or an explicit abstention.
type RootCause struct {
	Outcome     string   `json:"outcome"`
	Category    string   `json:"category"`
	Summary     string   `json:"summary"`
	Object      string   `json:"object"`
	Field       string   `json:"field"`
	EvidenceIDs []string `json:"evidence_ids"`
	NextChecks  []string `json:"next_checks"`
	Note        string   `json:"note,omitempty"` // set by code, e.g. on downgrade
}

// Fix is a proposed change. It is never applied to anything real.
type Fix struct {
	EvidenceID  string `json:"evidence_id"` // the manifest the diff targets
	Diff        string `json:"diff"`
	Explanation string `json:"explanation"`
	ApplyError  string `json:"apply_error,omitempty"`
}

// Finding is one policy rule result for a proposed fix.
type Finding struct {
	Rule        string `json:"rule"`
	Severity    string `json:"severity"` // warn or block
	Risk        string `json:"risk"`
	Alternative string `json:"alternative"`
}

// Review is the combined verdict of the policy rules and the critic.
type Review struct {
	Verdict       string    `json:"verdict"`
	PolicyVerdict string    `json:"policy_verdict"`
	CriticVerdict string    `json:"critic_verdict"`
	CriticReason  string    `json:"critic_reason"`
	Findings      []Finding `json:"findings"`
}

// Session is one diagnosis. It is what gets stored as JSON.
type Session struct {
	ID         string       `json:"id"`
	Target     Target       `json:"target"`
	Created    time.Time    `json:"created"`
	Status     string       `json:"status"`
	Step       string       `json:"step"`
	Error      string       `json:"error,omitempty"`
	Evidence   []Evidence   `json:"evidence"`
	Hypotheses []Hypothesis `json:"hypotheses"`
	ToolCalls  []ToolCall   `json:"tool_calls"`
	RootCause  *RootCause   `json:"root_cause,omitempty"`
	Fix        *Fix         `json:"fix,omitempty"`
	Review     *Review      `json:"review,omitempty"`
	Warnings   []string     `json:"warnings,omitempty"`
}

// FindEvidence returns the evidence with the given ID, or nil.
func (s *Session) FindEvidence(id string) *Evidence {
	for i := range s.Evidence {
		if s.Evidence[i].ID == id {
			return &s.Evidence[i]
		}
	}
	return nil
}

// ModelRequests counts tool requests made by the model, accepted or not.
func (s *Session) ModelRequests() int {
	n := 0
	for _, c := range s.ToolCalls {
		if c.By == "model" {
			n++
		}
	}
	return n
}

// Clone returns a copy that shares no slices with s, so a reader can hold
// it while the pipeline keeps appending to s.
func (s *Session) Clone() Session {
	c := *s
	c.Evidence = append([]Evidence(nil), s.Evidence...)
	c.Hypotheses = append([]Hypothesis(nil), s.Hypotheses...)
	c.ToolCalls = append([]ToolCall(nil), s.ToolCalls...)
	c.Warnings = append([]string(nil), s.Warnings...)
	if s.RootCause != nil {
		rc := *s.RootCause
		c.RootCause = &rc
	}
	if s.Fix != nil {
		f := *s.Fix
		c.Fix = &f
	}
	if s.Review != nil {
		r := *s.Review
		c.Review = &r
	}
	return c
}
