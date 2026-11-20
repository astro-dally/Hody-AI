package pipeline

import (
	"context"

	"hody/internal/model"
)

// approve completes the session so the user can see the cause, the cited
// evidence, the diff, the verdict and any warnings. Nothing is applied;
// the decision to change anything stays with the user.
func (e *Engine) approve(_ context.Context, s *model.Session) error {
	s.Status = model.StatusComplete
	verdict := ""
	if s.Review != nil {
		verdict = s.Review.Verdict
	}
	e.audit(s, "presented", map[string]string{"verdict": verdict})
	return nil
}
