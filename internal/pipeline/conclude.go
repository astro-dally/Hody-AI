package pipeline

import (
	"context"
	"fmt"
	"strings"

	"hody/internal/model"
)

// conclude asks for a root cause or an explicit abstention, then checks in
// code that every cited evidence ID exists. On a bad citation it retries
// once; if the retry is also bad, the result is downgraded to
// insufficient evidence.
func (e *Engine) conclude(ctx context.Context, s *model.Session) error {
	msgs := e.messages(concludeTask, sessionContext(s))
	for attempt := 0; attempt < 2; attempt++ {
		var a concludeAnswer
		if err := e.ask(ctx, s, "conclude", msgs, &a); err != nil {
			return err
		}
		valid, invalid := splitCitations(s, a.EvidenceIDs)
		if len(invalid) == 0 {
			s.RootCause = rootCause(a, valid)
			e.audit(s, "conclusion", map[string]string{"outcome": a.Outcome, "category": a.Category, "evidence": strings.Join(valid, ",")})
			return nil
		}
		if attempt == 1 {
			rc := rootCause(a, valid)
			rc.Outcome = model.OutcomeInsufficient
			rc.Note = fmt.Sprintf("Downgraded by Hody: the answer cited evidence that does not exist (%s), twice.", strings.Join(invalid, ", "))
			if len(rc.NextChecks) == 0 {
				rc.NextChecks = []string{"Review the cited evidence by hand; the model's citations could not be verified."}
			}
			s.RootCause = rc
			e.audit(s, "conclusion", map[string]string{"outcome": rc.Outcome, "downgraded": "invalid citations"})
			return nil
		}
		msgs = e.messages(concludeTask, sessionContext(s), fmt.Sprintf("NOTE: your previous answer cited evidence IDs that do not exist: %s. Existing IDs are %s. Cite only existing IDs.",
			strings.Join(invalid, ", "), strings.Join(evidenceIDs(s), ", ")))
	}
	return nil
}

func rootCause(a concludeAnswer, valid []string) *model.RootCause {
	return &model.RootCause{Outcome: a.Outcome, Category: a.Category, Summary: a.Summary, Object: a.Object,
		Field: a.Field, EvidenceIDs: valid, NextChecks: a.NextChecks}
}

// splitCitations separates cited IDs into existing and non-existing ones.
func splitCitations(s *model.Session, ids []string) (valid, invalid []string) {
	for _, id := range ids {
		if s.FindEvidence(id) != nil {
			valid = append(valid, id)
		} else {
			invalid = append(invalid, id)
		}
	}
	return valid, invalid
}

func evidenceIDs(s *model.Session) []string {
	ids := make([]string, len(s.Evidence))
	for i, ev := range s.Evidence {
		ids[i] = ev.ID
	}
	return ids
}
