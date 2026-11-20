package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"hody/internal/gateway"
	"hody/internal/llm"
	"hody/internal/model"
	"hody/internal/pipeline"
	"hody/internal/store"
	"hody/internal/tools"
	"hody/internal/tools/docker"
)

type diagnoseOpts struct {
	file, socket, namespace, kubeconfig string
	asJSON                              bool
}

func cmdDiagnose(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	var o diagnoseOpts
	fs.StringVar(&o.file, "file", "", "manifest or compose file to propose a fix against")
	fs.StringVar(&o.socket, "socket", defaultSocket(), "Docker socket path")
	fs.StringVar(&o.namespace, "n", "default", "Kubernetes namespace")
	fs.StringVar(&o.kubeconfig, "kubeconfig", "", "kubeconfig path (default: $KUBECONFIG or ~/.kube/config)")
	fs.BoolVar(&o.asJSON, "json", false, "print the session as JSON")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return errors.New("usage: hody diagnose docker <container> | hody diagnose k8s pod/<name> -n <namespace>")
	}
	target, err := parseTarget(pos[0], pos[1], o)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	s, err := diagnose(ctx, target, o)
	if s == nil {
		return err
	}
	if o.asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(s)
	} else {
		fmt.Fprint(stdout, report(s))
	}
	return err
}

func parseTarget(platform, name string, o diagnoseOpts) (model.Target, error) {
	t := model.Target{Platform: platform, File: o.file}
	if o.file != "" {
		abs, err := filepath.Abs(o.file)
		if err != nil {
			return t, err
		}
		t.File = abs
	}
	switch platform {
	case model.PlatformDocker:
		t.Container = strings.TrimPrefix(name, "/")
	case model.PlatformK8s:
		pod, ok := strings.CutPrefix(name, "pod/")
		if !ok {
			return t, errors.New(`Kubernetes targets look like "pod/<name>"`)
		}
		t.Pod, t.Namespace = pod, o.namespace
	default:
		return t, fmt.Errorf("unknown platform %q (use docker or k8s)", platform)
	}
	return t, nil
}

// diagnose wires the real tools and model for one target and runs it.
func diagnose(ctx context.Context, t model.Target, o diagnoseOpts) (*model.Session, error) {
	st, err := store.Open(dataDir())
	if err != nil {
		return nil, err
	}
	s := pipeline.NewSession(t)
	deps, err := liveDeps(t, o, st, filepath.Join(st.Dir(), "llm-requests", s.ID))
	if err != nil {
		return nil, err
	}
	return s, pipeline.New(deps).Run(ctx, s)
}

// liveDeps builds a gateway, registry and model client for a real target.
func liveDeps(t model.Target, o diagnoseOpts, st *store.Store, recordDir string) (pipeline.Deps, error) {
	client, err := llm.New(llmConfig(recordDir))
	if err != nil {
		return pipeline.Deps{}, err
	}
	gw := gateway.New()
	list := []tools.Tool{tools.FileTool()}
	switch t.Platform {
	case model.PlatformDocker:
		list = append(list, docker.Tools(docker.New(o.socket))...)
	default:
		k, err := k8sTools(o.kubeconfig)
		if err != nil {
			return pipeline.Deps{}, err
		}
		list = append(list, k...)
	}
	return pipeline.Deps{Tools: tools.NewRegistry(gw, t, list...), LLM: client, Gateway: gw, Store: st}, nil
}

func defaultSocket() string {
	if h := os.Getenv("DOCKER_HOST"); strings.HasPrefix(h, "unix://") {
		return strings.TrimPrefix(h, "unix://")
	}
	return "/var/run/docker.sock"
}
