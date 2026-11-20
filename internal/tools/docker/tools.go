package docker

import (
	"context"
	"strconv"
	"time"

	"hody/internal/tools"
)

// Tools binds the Docker tool specs to a read-only client. The binding
// lives here, not in package tools, so that package tools (and therefore
// the pipeline) never imports this client.
func Tools(c *Client) []tools.Tool {
	inspect := func(ctx context.Context, a map[string]string) (Container, error) {
		return c.Inspect(ctx, a["container"])
	}
	return tools.WithRuns(tools.DockerSpecs(), map[string]tools.RunFunc{
		"docker.list_containers": func(ctx context.Context, a map[string]string) (string, error) {
			s, err := c.ListContainers(ctx, a["container"])
			return FormatSummaries(s), err
		},
		"docker.inspect_container": func(ctx context.Context, a map[string]string) (string, error) {
			ct, err := inspect(ctx, a)
			return FormatInspect(ct), err
		},
		"docker.logs": func(ctx context.Context, a map[string]string) (string, error) {
			tail := 300
			if v, ok := a["tail"]; ok {
				tail, _ = strconv.Atoi(v)
			}
			return c.Logs(ctx, a["container"], tail)
		},
		"docker.events": func(ctx context.Context, a map[string]string) (string, error) {
			ev, err := c.Events(ctx, a["container"], time.Hour)
			return FormatEvents(ev), err
		},
		"docker.inspect_image": func(ctx context.Context, a map[string]string) (string, error) {
			ct, err := inspect(ctx, a)
			if err != nil {
				return "", err
			}
			img, err := c.InspectImage(ctx, ct.Config.Image)
			return FormatImage(img), err
		},
		"docker.config": func(ctx context.Context, a map[string]string) (string, error) {
			ct, err := inspect(ctx, a)
			return Compose(ct), err
		},
	})
}
