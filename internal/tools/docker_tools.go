package tools

import (
	"context"
	"fmt"
	"os"

	"hody/internal/model"
)

// RunFunc is the body of a tool.
type RunFunc func(ctx context.Context, args map[string]string) (string, error)

// DockerSpecs describes the Docker tools without binding them to a daemon.
func DockerSpecs() []Tool {
	c := Param{Name: "container", Required: true, Pattern: containerName, Doc: "the session's container"}
	d := model.PlatformDocker
	return []Tool{
		{Name: "docker.list_containers", Platform: d, Params: []Param{c}, Doc: "the container's line in the container list (state and status)"},
		{Name: "docker.inspect_container", Platform: d, Params: []Param{c}, Doc: "state, exit code, OOM flag, restart count, config, env, mounts, host settings"},
		{Name: "docker.logs", Platform: d, Params: []Param{c, {Name: "tail", Pattern: smallNumber, Doc: "lines, default 300"}}, Doc: "recent stdout and stderr with timestamps (all runs of the container)"},
		{Name: "docker.events", Platform: d, Params: []Param{c}, Doc: "engine events for the container in the last hour (start, die, oom, health)"},
		{Name: "docker.inspect_image", Platform: d, Params: []Param{c}, Doc: "metadata of the image the container runs (user, entrypoint, env)"},
		{Name: "docker.config", Platform: d, Params: []Param{c}, Doc: "the container's configuration as a compose-style file; fixes are diffs against it"},
	}
}

// DockerFirstLook is the fixed collect set for a Docker target.
var DockerFirstLook = []string{"docker.inspect_container", "docker.logs", "docker.events", "docker.config"}

// FileSpec is the tool that reads the manifest file the user named.
func FileSpec() Tool {
	return Tool{Name: "local.file", Platform: "any", Params: []Param{{Name: "path", Required: true, Pattern: anyPath, Doc: "the file the user supplied"}},
		Doc: "the manifest or compose file the user supplied; fixes are diffs against it"}
}

// FileTool reads the user's file (at most 1 MiB).
func FileTool() Tool {
	return WithRuns([]Tool{FileSpec()}, map[string]RunFunc{"local.file": func(_ context.Context, a map[string]string) (string, error) {
		st, err := os.Stat(a["path"])
		if err != nil {
			return "", err
		}
		if st.Size() > 1<<20 {
			return "", fmt.Errorf("file is larger than 1 MiB")
		}
		b, err := os.ReadFile(a["path"])
		return string(b), err
	}})[0]
}

// WithRuns attaches run functions to specs. A spec without one is a bug.
func WithRuns(specs []Tool, runs map[string]RunFunc) []Tool {
	for i := range specs {
		run, ok := runs[specs[i].Name]
		if !ok {
			panic("tools: no implementation for " + specs[i].Name)
		}
		specs[i].Run = run
	}
	return specs
}
