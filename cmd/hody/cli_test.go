package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hody/internal/llm"
)

func TestNoArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("script: code %d out %q", code, out.String())
	}
	old := isTerminal
	isTerminal = func(io.Writer) bool { return true }
	defer func() { isTerminal = old }()
	t.Setenv("NO_COLOR", "1")
	out.Reset()
	if code := run(nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "Hello from Hody") ||
		!strings.Contains(out.String(), "hody doctor") || strings.Contains(out.String(), "\033[") {
		t.Fatalf("terminal: code %d out %q", code, out.String())
	}
}

func TestVersionAndHelp(t *testing.T) {
	cases := []struct {
		args []string
		code int
		out  string
	}{
		{[]string{"version"}, 0, "hody dev (commit none"},
		{[]string{"--version"}, 0, "hody dev"},
		{[]string{"help"}, 0, "Commands:"},
		{[]string{"diagnose", "--help"}, 0, ""},
		{[]string{"doctor", "-h"}, 0, ""},
		{[]string{"doctor", "extra"}, 1, ""},
		{[]string{"frobnicate"}, 1, ""},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		if code := run(c.args, &out, &errOut); code != c.code || !strings.Contains(out.String(), c.out) {
			t.Errorf("%v: code %d out %q err %q", c.args, code, out.String(), errOut.String())
		}
	}
}

func TestFindSocket(t *testing.T) {
	cases := []struct {
		exists []string
		want   string
	}{
		{nil, "/var/run/docker.sock"},
		{[]string{"/var/run/docker.sock", "/home/u/.docker/run/docker.sock"}, "/var/run/docker.sock"},
		{[]string{"/home/u/.docker/run/docker.sock"}, "/home/u/.docker/run/docker.sock"},
		{[]string{"/home/u/.colima/default/docker.sock"}, "/home/u/.colima/default/docker.sock"},
		{[]string{"/home/u/.orbstack/run/docker.sock"}, "/home/u/.orbstack/run/docker.sock"},
	}
	for _, c := range cases {
		got := findSocket("/home/u", func(p string) bool { return strings.Contains(strings.Join(c.exists, " "), p) })
		if got != c.want {
			t.Errorf("exists %v: got %s, want %s", c.exists, got, c.want)
		}
	}
}

// doctor asks the model server for its list of models and sends no prompt;
// a password in the base URL is never printed.
func TestModelCheck(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+" "+string(b))
		switch r.URL.Path {
		case "/v1/models":
			io.WriteString(w, `{"data":[{"id":"qwen3:32b"},{"id":"llama3.1:latest"}]}`)
		default:
			http.Error(w, "no", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	withPassword := strings.Replace(srv.URL, "http://", "http://user:hunter2@", 1)
	cases := []struct {
		cfg  llm.Config
		want string
		ok   bool
	}{
		{llm.Config{}, "not configured", false},
		{llm.Config{BaseURL: srv.URL + "/v1", Model: "qwen3:32b", APIKey: "k"}, "qwen3:32b at " + srv.URL, true},
		{llm.Config{BaseURL: srv.URL + "/v1/", Model: "llama3.1"}, "llama3.1 at", true},
		{llm.Config{BaseURL: srv.URL + "/v1", Model: "mistral"}, "does not have mistral", false},
		{llm.Config{BaseURL: srv.URL + "/v2", Model: "qwen3:32b"}, "answered HTTP 500", false},
		{llm.Config{BaseURL: withPassword + "/v2", Model: "qwen3:32b"}, "user:xxxxx@", false},
		{llm.Config{BaseURL: "http://127.0.0.1:1/v1", Model: "qwen3:32b"}, "no model server", false},
	}
	for _, c := range cases {
		got, err := modelCheck(context.Background(), c.cfg, http.DefaultClient)
		msg := got
		if err != nil {
			msg = err.Error()
		}
		if (err == nil) != c.ok || !strings.Contains(msg, c.want) || strings.Contains(msg, "hunter2") {
			t.Errorf("%+v: got %q err %v", c.cfg, got, err)
		}
	}
	for _, b := range bodies {
		if !strings.HasPrefix(b, "GET ") || strings.HasSuffix(b, "}") {
			t.Errorf("doctor sent more than a model list request: %q", b)
		}
	}
	if !strings.Contains(bodies[0], "Bearer k") {
		t.Errorf("API key not sent: %q", bodies[0])
	}
}

func TestDoctor(t *testing.T) {
	ok := func(s string) func(context.Context) (string, error) {
		return func(context.Context) (string, error) { return s, nil }
	}
	bad := func(err error) func(context.Context) (string, error) {
		return func(context.Context) (string, error) { return "", err }
	}
	cases := []struct {
		name string
		c    checks
		code int
		want []string
	}{
		{"all good", checks{ok("reachable (3 containers)"), ok(`context "kind-kind"`), ok("qwen3:32b at x")}, 0,
			[]string{"✓ Docker", "✓ Kubernetes", "✓ AI model", "Ready: Hody can diagnose Docker containers and Kubernetes pods."}},
		{"docker only", checks{ok("reachable"), bad(errors.New("no cluster configured")), ok("m")}, 0,
			[]string{"✗ Kubernetes  no cluster configured", "only needed for pods", "Ready: Hody can diagnose Docker containers."}},
		{"no model", checks{ok("reachable"), ok("ctx"), bad(errNotConfigured)}, 1,
			[]string{"✗ AI model    not configured", "ollama pull", "Not ready: Hody needs an AI model"}},
		{"model server down", checks{ok("reachable"), ok("ctx"), bad(errors.New("no model server at x"))}, 1,
			[]string{"ollama list"}},
		{"nothing to diagnose", checks{bad(errors.New("not reachable")), bad(errors.New("no cluster")), ok("m")}, 1,
			[]string{"Not ready: Hody can reach neither Docker nor Kubernetes."}},
	}
	for _, c := range cases {
		var out bytes.Buffer
		err := cmdDoctor(nil, &out, func(string, string) checks { return c.c })
		code := 0
		if err != nil {
			code = 1
		}
		if code != c.code {
			t.Errorf("%s: code %d\n%s", c.name, code, out.String())
		}
		for _, w := range c.want {
			if !strings.Contains(out.String(), w) {
				t.Errorf("%s: missing %q in\n%s", c.name, w, out.String())
			}
		}
	}
}
