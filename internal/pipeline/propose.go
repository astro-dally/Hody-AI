package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"hody/internal/model"
	"hody/internal/policy"
	"hody/internal/tools"
)

// proposeFix asks for edits to the manifest evidence and builds the diff
// from them. If the edits do not apply, it asks once more, saying why; a
// second failure is kept with its reason, and the review blocks it. The
// step is skipped when there is no root cause or no manifest to change.
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
	parts := []string{cause, "MANIFEST:\n" + m.Block}
	for attempt := 0; ; attempt++ {
		var a fixAnswer
		if err := e.ask(ctx, s, "fix", e.messages(fmt.Sprintf(fixTask, m.ID), parts...), &a); err != nil {
			return err
		}
		s.Fix = &model.Fix{EvidenceID: m.ID, Edits: a.edits(), Explanation: a.Explanation}
		if s.Fix.Manual() {
			return nil
		}
		_, diff, err := policy.Patch(m.Text, manifestName(m), s.Fix.Edits)
		if err == nil {
			s.Fix.Diff = diff
			return nil
		}
		if attempt == 1 {
			s.Fix.ApplyError = err.Error()
			return nil
		}
		// The reason names edits and line numbers only, never manifest text.
		e.audit(s, "fix_retry", map[string]string{"reason": err.Error()})
		parts = []string{cause, "MANIFEST:\n" + m.Block,
			"NOTE: Hody could not apply your previous edits: " + err.Error() + `. Copy each "find" exactly from the manifest above.`}
	}
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

// manifestName is the file name shown in the diff header.
func manifestName(ev *model.Evidence) string {
	switch ev.Tool {
	case "local.file":
		return filepath.Base(ev.Args["path"])
	case "docker.config":
		return "docker-compose.yml"
	}
	return "manifest.yaml"
}
