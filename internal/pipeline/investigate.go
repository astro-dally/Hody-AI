package pipeline

import (
	"context"
	"fmt"

	"hody/internal/model"
)

// investigate lets the model request more read-only tools, up to
// MaxModelRequests. Every request is validated by the registry before it
// runs; a rejected request counts against the budget and the reason goes
// back to the model in the next prompt.
func (e *Engine) investigate(ctx context.Context, s *model.Session) error {
	for s.ModelRequests() < MaxModelRequests {
		left := MaxModelRequests - s.ModelRequests()
		var a investigateAnswer
		msgs := e.messages(fmt.Sprintf(investigateTask, left), sessionContext(s), "AVAILABLE TOOLS:\n"+e.d.Tools.Describe())
		if err := e.ask(ctx, s, "investigate", msgs, &a); err != nil {
			return err
		}
		if a.Action == "done" {
			return nil
		}
		_ = e.call(ctx, s, "model", a.Tool, a.Args) // rejection is recorded on the session
	}
	s.Warnings = append(s.Warnings, fmt.Sprintf("investigation stopped at the limit of %d tool requests", MaxModelRequests))
	return nil
}
