package pipeline

import (
	"context"
	"errors"

	"hody/internal/model"
	"hody/internal/tools"
)

// collect runs the fixed first-look tools for the target. A request the
// registry rejects here is a bug in the first-look set and stops the run.
func (e *Engine) collect(ctx context.Context, s *model.Session) error {
	for _, r := range tools.FirstLook(s.Target) {
		if err := e.call(ctx, s, "collect", r.Tool, r.Args); err != nil {
			return err
		}
	}
	if len(s.Evidence) == 0 {
		return errors.New("no evidence collected")
	}
	return nil
}
