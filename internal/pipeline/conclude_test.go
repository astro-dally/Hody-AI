package pipeline

import (
	"context"
	"strings"
	"testing"

	"hody/internal/llm"
	"hody/internal/model"
)

// between returns the part of s from start up to (not including) end.
func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	s = s[i:]
	if j := strings.Index(s, end); j >= 0 {
		s = s[:j]
	}
	return s
}

func concludeAs(category, field string, ids ...string) llm.Scripted {
	return answer("conclude", map[string]any{"symptom": "exit code 1 (E1)", "outcome": "root_cause", "category": category,
		"summary": "the script lacks execute permission", "object": "container web", "field": field, "evidence_ids": ids, "next_checks": []string{}})
}

var abstainAnswer = answer("conclude", map[string]any{"symptom": "exit code 1 (E1)", "outcome": "insufficient_evidence", "category": "",
	"summary": "", "object": "", "field": "", "evidence_ids": []string{"E1"}, "next_checks": []string{"run the script by hand"}})

// From live run 3: in the abstention scenario the container exits with
// code 1 after logging "starting" and "exiting", and the model concluded
// command_not_executable, a failure that always shows exit code 126 or
// "permission denied". Hody now checks a known category's signature
// against the evidence: one retry with a note, then a downgrade.
func TestConcludeChecksFailureSignature(t *testing.T) {
	unsupported := concludeAs("command_not_executable", "command", "E1", "E2")
	cases := []struct {
		name, logs string
		answers    []llm.Scripted
		outcome    string
		downgraded bool
	}{
		{"invented cause is downgraded", "starting\nexiting", []llm.Scripted{unsupported, unsupported}, model.OutcomeInsufficient, true},
		{"model abstains after the note", "starting\nexiting", []llm.Scripted{unsupported, abstainAnswer}, model.OutcomeInsufficient, false},
		{"signature in the evidence", "exec /app/start.sh: permission denied", []llm.Scripted{unsupported}, model.OutcomeRootCause, false},
		// Hody only knows its own categories' signatures; a cause under a
		// name of the model's own is not checked (a known limit).
		{"own category is not checked", "starting\nexiting", []llm.Scripted{concludeAs("script_error", "command", "E1", "E2")}, model.OutcomeRootCause, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, srv := harness(t, c.logs, c.answers...)
			s := NewSession(target)
			if err := e.collect(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if err := e.conclude(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			rc := s.RootCause
			if rc.Outcome != c.outcome || (rc.Note != "") != c.downgraded {
				t.Fatalf("got %+v", rc)
			}
			if c.downgraded && (!strings.Contains(rc.Note, "command_not_executable") || !strings.Contains(rc.Note, "exit code 126")) {
				t.Fatalf("note %q", rc.Note)
			}
			if len(c.answers) == 2 {
				note := between(string(srv.Bodies[1]), "NOTE: Hody checked", "TASK:")
				if !strings.Contains(note, "nothing in the evidence shows that failure") || strings.Contains(note, "starting") {
					t.Fatalf("retry note %q", note)
				}
			}
			if len(srv.Unused()) != 0 {
				t.Fatalf("unused answers %v", srv.Unused())
			}
		})
	}
}

// From live runs 2 and 3 (k8s-exit-126): the evidence shows exit code 126,
// and the model named the right cause under the neighbouring category
// mount_permission. That must never be downgraded: Hody names the failure
// the evidence shows, and keeps a second such answer with a warning.
func TestConcludeNeighbourCategoryIsNotDowngraded(t *testing.T) {
	logs := "exec /scripts/run.sh: permission denied"
	cases := []struct {
		name     string
		answers  []llm.Scripted
		category string
		warnings int
	}{
		{"kept, with a warning", []llm.Scripted{concludeAs("mount_permission", "defaultMode", "E1", "E2"), concludeAs("mount_permission", "defaultMode", "E1", "E2")}, "mount_permission", 1},
		{"renamed after the note", []llm.Scripted{concludeAs("mount_permission", "defaultMode", "E1", "E2"), concludeAs("command_not_executable", "defaultMode", "E1", "E2")}, "command_not_executable", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, srv := harness(t, logs, c.answers...)
			s := NewSession(target)
			if err := e.collect(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if err := e.conclude(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			rc := s.RootCause
			if rc.Outcome != model.OutcomeRootCause || rc.Note != "" || rc.Category != c.category || len(s.Warnings) != c.warnings {
				t.Fatalf("root cause %+v warnings %v", rc, s.Warnings)
			}
			note := between(string(srv.Bodies[1]), "NOTE: Hody checked", "TASK:")
			if !strings.Contains(note, "signature of a different known failure: command_not_executable") || strings.Contains(note, "run.sh") {
				t.Fatalf("retry note %q", note)
			}
		})
	}
}

// A downgraded answer gets no fix: the propose step needs a root cause.
func TestDowngradedAnswerGetsNoFix(t *testing.T) {
	unsupported := concludeAs("command_not_executable", "command", "E1")
	tri := answer("triage", map[string]any{"hypotheses": []map[string]any{{"category": "unknown", "summary": "s", "evidence_ids": []string{"E1"}}}})
	e, srv := harness(t, "starting\nexiting", tri, done(), unsupported, unsupported)
	s := NewSession(target)
	if err := e.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if s.RootCause.Outcome != model.OutcomeInsufficient || s.Fix != nil || s.Review != nil || len(srv.Missing) != 0 {
		t.Fatalf("root cause %+v fix %+v missing %v", s.RootCause, s.Fix, srv.Missing)
	}
}

// From live run 3 (docker-mount-permission): a right answer that cited
// only the logs. Hody asks once for the evidence that shows the failure
// and the faulty setting; a missing citation alone never downgrades.
func TestConcludeAsksForMissingCitations(t *testing.T) {
	cases := []struct {
		name     string
		answers  []llm.Scripted
		noteHas  string
		warnings int
	}{
		{"setting not cited, then cited", []llm.Scripted{cause("E2"), cause("E2", "E4")}, "setting mem_limit, which appears in E4", 0},
		{"failure not cited, then cited", []llm.Scripted{cause("E4"), cause("E2", "E4")}, "failure it names is shown in E2", 0},
		{"still not cited: kept, with a warning", []llm.Scripted{cause("E2"), cause("E2")}, "E4", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, srv := harness(t, "Killed exitCode=137 unique-log-marker", c.answers...)
			s := NewSession(target)
			if err := e.collect(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if err := e.conclude(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if s.RootCause.Outcome != model.OutcomeRootCause || s.RootCause.Note != "" || len(s.Warnings) != c.warnings {
				t.Fatalf("root cause %+v warnings %v", s.RootCause, s.Warnings)
			}
			note := between(string(srv.Bodies[1]), "NOTE: Hody checked", "TASK:")
			if !strings.Contains(note, c.noteHas) || !strings.Contains(note, "keep it and add") || strings.Contains(note, "unique-log-marker") {
				t.Fatalf("retry note %q", note)
			}
		})
	}
}

func TestSettingName(t *testing.T) {
	cases := map[string]string{
		"spec.template.spec.containers[api].resources.limits.memory": "memory",
		"volumes[/tmp/hody-uploads:/data]":                           "volumes",
		"environment.DB_HOST":                                        "DB_HOST",
		"mem_limit":                                                  "mem_limit",
		"":                                                           "",
		"[0]":                                                        "",
		"env[DATABASE_URL]":                                          "env",
	}
	for in, want := range cases {
		if got := settingName(in); got != want {
			t.Errorf("settingName(%q) = %q, want %q", in, got, want)
		}
	}
}
