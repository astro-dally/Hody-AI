package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hody/internal/llm"
	"hody/internal/tools/docker"
	"hody/internal/tools/k8s"
)

// errChecksFailed means doctor found a problem and already explained it.
var errChecksFailed = errors.New("some checks failed")

// checks are doctor's three probes, swappable for tests. Each returns a
// short description of what it found, or an error.
type checks struct {
	docker, k8s, model func(context.Context) (string, error)
}

var errNotConfigured = errors.New("not configured")

// cmdDoctor checks what Hody needs and says how to fix what is missing.
// It only reads: it lists containers, asks the cluster for its version
// and asks the model server which models it has. Nothing goes to a model.
func cmdDoctor(args []string, stdout io.Writer, mk func(socket, kubeconfig string) checks) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	commandUsage(fs, "hody doctor [--socket path] [--kubeconfig path]",
		"Checks that Hody can reach Docker, Kubernetes and the AI model, and says what\nto do if not. It only reads, and sends nothing to the model.")
	socket := fs.String("socket", defaultSocket(), "Docker socket path")
	kubeconfig := fs.String("kubeconfig", "", "kubeconfig path (default: $KUBECONFIG or ~/.kube/config)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("doctor takes no arguments (got %q)", fs.Arg(0))
	}
	c := mk(*socket, *kubeconfig)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	color := useColor(stdout)
	mark := func(err error) string {
		switch {
		case err == nil && color:
			return "\033[32m✓\033[0m"
		case err == nil:
			return "✓"
		case color:
			return "\033[31m✗\033[0m"
		}
		return "✗"
	}
	report := func(name, found string, err error, hint string) {
		if err == nil {
			fmt.Fprintf(stdout, "  %s %-11s %s\n", mark(nil), name, found)
			return
		}
		fmt.Fprintf(stdout, "  %s %-11s %s\n", mark(err), name, err)
		for i, h := range strings.Split(hint, "\n") {
			arrow := "→"
			if i > 0 {
				arrow = " "
			}
			fmt.Fprintf(stdout, "    %-11s %s %s\n", "", arrow, h)
		}
	}

	fmt.Fprintln(stdout, "Hody doctor")
	fmt.Fprintln(stdout)
	dockerFound, dockerErr := c.docker(ctx)
	report("Docker", dockerFound, dockerErr,
		"start Docker (Docker Desktop, Colima, OrbStack...), or pass --socket or set DOCKER_HOST=unix:///path/to/docker.sock")
	k8sFound, k8sErr := c.k8s(ctx)
	report("Kubernetes", k8sFound, k8sErr,
		"only needed for pods: check that \"kubectl get pods\" works, or pass --kubeconfig")
	modelFound, modelErr := c.model(ctx)
	modelHint := "set HODY_LLM_BASE_URL and HODY_LLM_MODEL; for Ollama on this machine:\nollama pull qwen3:32b\nexport HODY_LLM_BASE_URL=http://localhost:11434/v1 HODY_LLM_MODEL=qwen3:32b"
	if !errors.Is(modelErr, errNotConfigured) {
		modelHint = "check that the model server is running and the model is pulled (for Ollama: ollama list)"
	}
	report("AI model", modelFound, modelErr, modelHint)
	fmt.Fprintln(stdout)

	var can []string
	if dockerErr == nil {
		can = append(can, "Docker containers")
	}
	if k8sErr == nil {
		can = append(can, "Kubernetes pods")
	}
	switch {
	case modelErr != nil:
		fmt.Fprintln(stdout, "Not ready: Hody needs an AI model to diagnose anything.")
	case len(can) == 0:
		fmt.Fprintln(stdout, "Not ready: Hody can reach neither Docker nor Kubernetes.")
	default:
		fmt.Fprintf(stdout, "Ready: Hody can diagnose %s.\n", strings.Join(can, " and "))
		return nil
	}
	return errChecksFailed
}

// realChecks probes the real Docker daemon, cluster and model server.
func realChecks() func(socket, kubeconfig string) checks {
	return func(socket, kubeconfig string) checks {
		return checks{
			docker: func(ctx context.Context) (string, error) {
				list, err := docker.New(socket).ListContainers(ctx, "")
				if err != nil {
					return "", fmt.Errorf("not reachable at %s", socket)
				}
				return fmt.Sprintf("reachable at %s (%d containers)", socket, len(list)), nil
			},
			k8s: func(ctx context.Context) (string, error) {
				c, err := k8s.New(kubeconfig)
				if err != nil {
					return "", errors.New("no cluster configured")
				}
				v, err := c.ServerVersion(ctx)
				name := k8s.CurrentContext(kubeconfig)
				if err != nil {
					return "", fmt.Errorf("context %q is not reachable", name)
				}
				return fmt.Sprintf("context %q, Kubernetes %s", name, v), nil
			},
			model: func(ctx context.Context) (string, error) {
				return modelCheck(ctx, llmConfig(""), http.DefaultClient)
			},
		}
	}
}

// modelCheck asks an OpenAI-compatible server which models it has (GET
// /models) and looks for the configured one. It sends no prompt.
func modelCheck(ctx context.Context, cfg llm.Config, hc *http.Client) (string, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return "", errNotConfigured
	}
	shown := cfg.BaseURL
	if u, err := url.Parse(cfg.BaseURL); err == nil {
		shown = u.Redacted()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.BaseURL, "/")+"/models", nil)
	if err != nil {
		return "", fmt.Errorf("bad HODY_LLM_BASE_URL %q", shown)
	}
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("no model server at %s", shown)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("model server at %s answered HTTP %d", shown, resp.StatusCode)
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&list); err != nil {
		return "", fmt.Errorf("model server at %s did not list its models", shown)
	}
	for _, m := range list.Data {
		if m.ID == cfg.Model || m.ID == cfg.Model+":latest" {
			return fmt.Sprintf("%s at %s", cfg.Model, shown), nil
		}
	}
	return "", fmt.Errorf("the server at %s does not have %s", shown, cfg.Model)
}

// defaultSocket is DOCKER_HOST if it names a Unix socket, otherwise the
// first Docker socket that exists.
func defaultSocket() string {
	if h := os.Getenv("DOCKER_HOST"); strings.HasPrefix(h, "unix://") {
		return strings.TrimPrefix(h, "unix://")
	}
	home, _ := os.UserHomeDir()
	return findSocket(home, func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && st.Mode()&os.ModeSocket != 0
	})
}

// findSocket tries the standard path, then where Docker Desktop, Colima,
// OrbStack and Rancher Desktop put their sockets on a Mac.
func findSocket(home string, exists func(string) bool) string {
	candidates := []string{"/var/run/docker.sock"}
	if home != "" {
		for _, p := range []string{".docker/run/docker.sock", ".colima/default/docker.sock", ".orbstack/run/docker.sock", ".rd/docker.sock"} {
			candidates = append(candidates, filepath.Join(home, p))
		}
	}
	for _, c := range candidates {
		if exists(c) {
			return c
		}
	}
	return candidates[0]
}
