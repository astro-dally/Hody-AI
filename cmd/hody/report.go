package main

import (
	"fmt"
	"strings"

	"hody/internal/model"
)

// report renders a finished session as plain text for the terminal.
func report(s *model.Session) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("Hody diagnosis %s for %s: %s", s.ID, s.Target, s.Status)
	if s.Error != "" {
		w("Error: %s", s.Error)
	}
	if rc := s.RootCause; rc != nil {
		if rc.Symptom != "" {
			w("\nWhat failed: %s", rc.Symptom)
		}
		if rc.Outcome == model.OutcomeRootCause {
			w("\nRoot cause (%s): %s", rc.Category, rc.Summary)
			w("Faulty object: %s   field: %s", rc.Object, rc.Field)
		} else {
			w("\nNot enough evidence to name a root cause.")
			if rc.Summary != "" {
				w("%s", rc.Summary)
			}
		}
		if rc.Note != "" {
			w("Note: %s", rc.Note)
		}
		for _, c := range rc.NextChecks {
			w("Next check: %s", c)
		}
		w("\nCited evidence:")
		for _, id := range rc.EvidenceIDs {
			if ev := s.FindEvidence(id); ev != nil {
				w("--- %s from %s %v ---\n%s", ev.ID, ev.Tool, ev.Args, indent(ev.Text, 40))
			}
		}
	}
	if f := s.Fix; f != nil {
		switch {
		case f.Manual():
			w("\nProposed fix: no change to the configuration can fix this. Steps to do by hand:\n%s", f.Explanation)
		case f.ApplyError != "":
			w("\nProposed fix: the model's edits could not be applied to %s (%s), so there is no diff to show.", f.EvidenceID, f.ApplyError)
			w("What it meant to do: %s", f.Explanation)
		default:
			w("\nProposed fix (not applied) against %s:\n%s", f.EvidenceID, f.Diff)
			w("Why: %s", f.Explanation)
		}
	}
	if r := s.Review; r != nil {
		w("\nReview verdict: %s (policy: %s, critic: %s)", strings.ToUpper(r.Verdict), r.PolicyVerdict, r.CriticVerdict)
		if r.CriticReason != "" {
			w("Critic: %s", r.CriticReason)
		}
		for _, f := range r.Findings {
			w("[%s] %s\n  Risk: %s\n  Safer: %s", f.Severity, f.Rule, f.Risk, f.Alternative)
		}
	}
	flags := 0
	for _, ev := range s.Evidence {
		flags += len(ev.Flags)
	}
	if flags > 0 {
		w("\n%d log line(s) looked like instructions aimed at an AI; they were treated as data only.", flags)
	}
	for _, warn := range s.Warnings {
		w("Warning: %s", warn)
	}
	return b.String()
}

// indent shows at most max lines of text, indented.
func indent(text string, max int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > max {
		lines = append(lines[:max], fmt.Sprintf("… %d more lines in the session file", len(lines)-max))
	}
	return "  " + strings.Join(lines, "\n  ")
}
