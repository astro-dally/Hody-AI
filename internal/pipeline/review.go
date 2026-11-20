package pipeline

import (
	"context"
	"fmt"

	"hody/internal/model"
	"hody/internal/policy"
)

// reviewFix applies the diff to an in-memory copy of the manifest, runs the
// policy rules on the original and the patched text, then asks a separate
// critic call that sees only the root cause and the diff (never logs or
// evidence). The stricter of the two verdicts wins.
func (e *Engine) reviewFix(ctx context.Context, s *model.Session) error {
	if s.Fix == nil {
		return nil
	}
	r := &model.Review{}
	original := s.FindEvidence(s.Fix.EvidenceID).Text
	if patched, err := policy.Apply(original, s.Fix.Diff); err != nil {
		s.Fix.ApplyError = err.Error()
		r.Findings = []model.Finding{policy.NotApplied(err.Error())}
	} else {
		r.Findings = policy.Check(original, patched)
	}
	r.PolicyVerdict = policy.Verdict(r.Findings)

	rc := s.RootCause
	cause := fmt.Sprintf("STATED ROOT CAUSE (%s): %s\nFaulty object: %s, field: %s", rc.Category, rc.Summary, rc.Object, rc.Field)
	var a criticAnswer
	if err := e.ask(ctx, s, "critic", e.messages(criticTask, cause, "PROPOSED DIFF:\n"+s.Fix.Diff), &a); err != nil {
		return err
	}
	r.CriticVerdict, r.CriticReason = a.Verdict, a.Reason
	r.Verdict = policy.Stricter(r.PolicyVerdict, r.CriticVerdict)
	s.Review = r
	e.audit(s, "review", map[string]string{"verdict": r.Verdict, "policy": r.PolicyVerdict, "critic": r.CriticVerdict})
	return nil
}
