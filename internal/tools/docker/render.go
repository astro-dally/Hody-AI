package docker

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Summary is one entry from /containers/json.
type Summary struct {
	ID     string `json:"Id"`
	Names  []string
	Image  string
	State  string
	Status string
}

// Container is the subset of /containers/{id}/json that Hody reads.
type Container struct {
	ID           string `json:"Id"`
	Name         string
	RestartCount int
	State        struct {
		Status     string
		OOMKilled  bool
		ExitCode   int
		Error      string
		StartedAt  string
		FinishedAt string
		Health     *struct {
			Status        string
			FailingStreak int
			Log           []struct {
				ExitCode int
				Output   string
			}
		}
	}
	Config struct {
		Image        string
		User         string
		Env          []string
		Cmd          []string
		Entrypoint   []string
		WorkingDir   string
		ExposedPorts map[string]struct{}
		Healthcheck  *struct{ Test []string }
	}
	HostConfig struct {
		Privileged     bool
		NetworkMode    string
		PidMode        string
		Binds          []string
		CapAdd         []string
		Memory         int64
		ReadonlyRootfs bool
		PortBindings   map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string
		}
		RestartPolicy struct{ Name string }
	}
	Mounts []struct {
		Type, Source, Destination, Mode string
		RW                              bool
	}
}

// Image is the subset of /images/{name}/json that Hody reads.
type Image struct {
	ID           string `json:"Id"`
	RepoTags     []string
	Created      string
	Architecture string
	Os           string
	Config       struct {
		User       string
		Entrypoint []string
		Cmd        []string
		WorkingDir string
		Env        []string
	}
}

// Event is one engine event.
type Event struct {
	Type   string
	Action string
	Time   int64 `json:"time"`
	Actor  struct{ Attributes map[string]string }
}

func list(v []string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// FormatSummaries renders a container list.
func FormatSummaries(s []Summary) string {
	if len(s) == 0 {
		return "no container with that name"
	}
	var b strings.Builder
	for _, c := range s {
		fmt.Fprintf(&b, "%s names=%s image=%s state=%s status=%q\n", short(c.ID), list(c.Names), c.Image, c.State, c.Status)
	}
	return b.String()
}

// FormatInspect renders the parts of a container that matter for diagnosis.
func FormatInspect(c Container) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	w("container: %s (id %s)", strings.TrimPrefix(c.Name, "/"), short(c.ID))
	w("state: status=%s exitCode=%d oomKilled=%t restartCount=%d", c.State.Status, c.State.ExitCode, c.State.OOMKilled, c.RestartCount)
	w("startedAt=%s finishedAt=%s", c.State.StartedAt, c.State.FinishedAt)
	if c.State.Error != "" {
		w("stateError: %s", c.State.Error)
	}
	if h := c.State.Health; h != nil {
		w("health: status=%s failingStreak=%d", h.Status, h.FailingStreak)
		for _, l := range h.Log {
			w("healthcheck exitCode=%d output=%q", l.ExitCode, strings.TrimSpace(l.Output))
		}
	}
	w("image: %s", c.Config.Image)
	w("user: %q workingDir: %q", c.Config.User, c.Config.WorkingDir)
	w("entrypoint: %s cmd: %s", list(c.Config.Entrypoint), list(c.Config.Cmd))
	for _, e := range c.Config.Env {
		w("env: %s", e)
	}
	w("exposedPorts: %s", list(keys(c.Config.ExposedPorts)))
	for _, port := range keys(c.HostConfig.PortBindings) {
		for _, pb := range c.HostConfig.PortBindings[port] {
			w("portBinding: %s:%s -> %s", pb.HostIP, pb.HostPort, port)
		}
	}
	for _, m := range c.Mounts {
		w("mount: type=%s source=%s destination=%s rw=%t", m.Type, m.Source, m.Destination, m.RW)
	}
	h := c.HostConfig
	w("hostConfig: privileged=%t networkMode=%s pidMode=%q capAdd=%s memoryBytes=%d readonlyRootfs=%t restartPolicy=%s",
		h.Privileged, h.NetworkMode, h.PidMode, list(h.CapAdd), h.Memory, h.ReadonlyRootfs, h.RestartPolicy.Name)
	return b.String()
}

// FormatImage renders image metadata.
func FormatImage(i Image) string {
	return fmt.Sprintf("image: %s tags=%s created=%s platform=%s/%s\nuser: %q workingDir: %q\nentrypoint: %s cmd: %s\nenv: %s\n",
		short(i.ID), list(i.RepoTags), i.Created, i.Os, i.Architecture, i.Config.User, i.Config.WorkingDir,
		list(i.Config.Entrypoint), list(i.Config.Cmd), list(i.Config.Env))
}

// FormatEvents renders engine events, oldest first.
func FormatEvents(ev []Event) string {
	if len(ev) == 0 {
		return "no events in the window"
	}
	var b strings.Builder
	for _, e := range ev {
		attrs := make([]string, 0, len(e.Actor.Attributes))
		for _, k := range keys(e.Actor.Attributes) {
			attrs = append(attrs, k+"="+e.Actor.Attributes[k])
		}
		fmt.Fprintf(&b, "%s %s %s %s\n", time.Unix(e.Time, 0).UTC().Format(time.RFC3339), e.Type, e.Action, strings.Join(attrs, " "))
	}
	return b.String()
}

// Compose renders the container's configuration as a docker-compose style
// file. This is the "manifest" a fix diff targets when the user gives no
// file of their own. It is generated text; Hody never applies it.
func Compose(c Container) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	name := strings.TrimPrefix(c.Name, "/")
	w("# Generated by Hody from the container's configuration. Hody never applies changes.")
	w("services:")
	w("  %s:", name)
	w("    image: %s", c.Config.Image)
	if c.Config.User != "" {
		w("    user: %q", c.Config.User)
	}
	if len(c.Config.Entrypoint) > 0 {
		w("    entrypoint: %s", list(c.Config.Entrypoint))
	}
	if len(c.Config.Cmd) > 0 {
		w("    command: %s", list(c.Config.Cmd))
	}
	section := func(title string, items []string) {
		if len(items) > 0 {
			w("    %s:", title)
			for _, it := range items {
				w("      - %s", it)
			}
		}
	}
	section("environment", c.Config.Env)
	var ports []string
	for port, binds := range c.HostConfig.PortBindings {
		for _, pb := range binds {
			ports = append(ports, fmt.Sprintf("%q", pb.HostPort+":"+strings.TrimSuffix(port, "/tcp")))
		}
	}
	sort.Strings(ports)
	section("ports", ports)
	section("volumes", c.HostConfig.Binds)
	h := c.HostConfig
	if h.Privileged {
		w("    privileged: true")
	}
	if h.NetworkMode == "host" {
		w("    network_mode: host")
	}
	if h.PidMode == "host" {
		w("    pid: host")
	}
	section("cap_add", h.CapAdd)
	if h.Memory > 0 {
		w("    mem_limit: %d", h.Memory)
	}
	if c.Config.Healthcheck != nil && len(c.Config.Healthcheck.Test) > 0 {
		w("    healthcheck:")
		w("      test: %s", list(c.Config.Healthcheck.Test))
	}
	return b.String()
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func short(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
