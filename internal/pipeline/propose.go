package pipeline

import (
	"context"
	"fmt"
	"strings"

	"hody/internal/model"
	"hody/internal/tools"
)

// proposeFix asks for a unified diff against the manifest evidence. It is
// skipped when there is no root cause or no manifest to change.
func (e *Engine) proposeFix(ctx context.Context, s *model.Session) error {
	if s.RootCause == nil || s.RootCause.Outcome != model.OutcomeRootCause {
		return nil
	}
	m := manifest(s)
	if m == nil {
		s.Warnings = append(s.Warnings, "no manifest or configuration file was available, so no fix was proposed")
		return nil
	}
	rc := s.RootCause
	cause := fmt.Sprintf("ROOT CAUSE (%s): %s\nFaulty object: %s, field: %s", rc.Category, rc.Summary, rc.Object, rc.Field)
	var a fixAnswer
	if err := e.ask(ctx, s, "fix", e.messages(fmt.Sprintf(fixTask, m.ID), cause, "MANIFEST:\n"+m.Block), &a); err != nil {
		return err
	}
	s.Fix = &model.Fix{EvidenceID: m.ID, Diff: a.Diff, Explanation: a.Explanation}
	return nil
}

// manifest returns the best evidence to fix: the user's file if given,
// otherwise the generated manifest.
func manifest(s *model.Session) *model.Evidence {
	for _, name := range tools.ManifestTools {
		for i := range s.Evidence {
			ev := &s.Evidence[i]
			if ev.Tool == name && ev.Text != "" && !strings.HasPrefix(ev.Text, "tool error") {
				return ev
			}
		}
	}
	return nil
}
