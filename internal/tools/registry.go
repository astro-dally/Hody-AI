// Package tools is the registry of read-only tools. It validates every
// request (tool known, arguments match the schema, target inside the
// session's scope) before running it, and it returns results only as
// gateway evidence, so callers never see raw tool output.
package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"hody/internal/gateway"
	"hody/internal/model"
)

// Param describes one tool argument.
type Param struct {
	Name     string
	Required bool
	Pattern  *regexp.Regexp
	Doc      string
}

// Tool is one read-only operation. Run returns raw text, which only the
// registry ever sees.
type Tool struct {
	Name     string
	Platform string // model.PlatformDocker, model.PlatformK8s or "any"
	Doc      string
	Params   []Param
	Run      func(ctx context.Context, args map[string]string) (string, error)
}

// Argument patterns shared by tool specs.
var (
	k8sName       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	containerName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	smallNumber   = regexp.MustCompile(`^[0-9]{1,4}$`)
	boolean       = regexp.MustCompile(`^(true|false)$`)
	anyPath       = regexp.MustCompile(`^[^\x00]{1,1000}$`)
)

// scopedParams must equal the session target. This is the scope lock.
var scopedParams = map[string]func(model.Target) string{
	"namespace": func(t model.Target) string { return t.Namespace },
	"container": func(t model.Target) string { return t.Container },
	"path":      func(t model.Target) string { return t.File },
}

// Registry runs validated tool requests for one session.
type Registry struct {
	tools map[string]Tool
	scope model.Target
	gw    *gateway.Gateway
}

// NewRegistry fixes the scope for the life of the registry.
func NewRegistry(gw *gateway.Gateway, scope model.Target, list ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}, scope: scope, gw: gw}
	for _, t := range list {
		r.tools[t.Name] = t
	}
	return r
}

// Validate checks a request without running it.
func (r *Registry) Validate(name string, args map[string]string) error {
	t, ok := r.tools[name]
	if !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	if t.Platform != "any" && t.Platform != r.scope.Platform {
		return fmt.Errorf("tool %s is for %s, this session is %s", name, t.Platform, r.scope.Platform)
	}
	known := map[string]Param{}
	for _, p := range t.Params {
		known[p.Name] = p
		if _, ok := args[p.Name]; p.Required && !ok {
			return fmt.Errorf("tool %s: missing argument %q", name, p.Name)
		}
	}
	for k, v := range args {
		p, ok := known[k]
		if !ok {
			return fmt.Errorf("tool %s: unknown argument %q", name, k)
		}
		if !p.Pattern.MatchString(v) {
			return fmt.Errorf("tool %s: argument %q has an invalid value", name, k)
		}
		if want, scoped := scopedParams[k]; scoped && v != want(r.scope) {
			return fmt.Errorf("tool %s: argument %q is outside this session's scope (%s)", name, k, r.scope)
		}
	}
	return nil
}

// Call validates and runs a request and returns its evidence. A rejected
// request returns an error and runs nothing. A tool that runs but fails
// produces evidence describing the failure, because failures are clues.
func (r *Registry) Call(ctx context.Context, name string, args map[string]string) (model.Evidence, error) {
	if err := r.Validate(name, args); err != nil {
		return model.Evidence{}, err
	}
	out, err := r.tools[name].Run(ctx, args)
	if err != nil {
		out = "tool error: " + err.Error()
	}
	return r.gw.Process(name, args, out), nil
}

// Describe lists the tools usable in this session, for the model prompt.
func (r *Registry) Describe() string {
	names := make([]string, 0, len(r.tools))
	for n, t := range r.tools {
		if t.Platform == "any" || t.Platform == r.scope.Platform {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		t := r.tools[n]
		var ps []string
		for _, p := range t.Params {
			s := p.Name
			if !p.Required {
				s += "?"
			}
			if v, scoped := scopedParams[p.Name]; scoped {
				s += `="` + v(r.scope) + `"`
			}
			ps = append(ps, s)
		}
		fmt.Fprintf(&b, "- %s(%s): %s\n", n, strings.Join(ps, ", "), t.Doc)
	}
	return b.String()
}
