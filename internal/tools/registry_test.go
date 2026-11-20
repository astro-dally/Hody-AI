package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hody/internal/gateway"
	"hody/internal/model"
)

func fakeTool(name, platform string, calls *int, params ...Param) Tool {
	return Tool{Name: name, Platform: platform, Params: params, Doc: "test",
		Run: func(context.Context, map[string]string) (string, error) { *calls++; return "ok", nil }}
}

func TestValidate(t *testing.T) {
	var calls int
	ns := Param{Name: "namespace", Required: true, Pattern: k8sName}
	pod := Param{Name: "pod", Required: true, Pattern: k8sName}
	prev := Param{Name: "previous", Pattern: boolean}
	ctr := Param{Name: "container", Required: true, Pattern: containerName}
	k8s := model.Target{Platform: model.PlatformK8s, Namespace: "shop", Pod: "api"}
	r := NewRegistry(gateway.New(), k8s,
		fakeTool("k8s.logs", model.PlatformK8s, &calls, ns, pod, prev),
		fakeTool("docker.logs", model.PlatformDocker, &calls, ctr))
	cases := []struct {
		name, tool string
		args       map[string]string
		errPart    string
	}{
		{"valid", "k8s.logs", map[string]string{"namespace": "shop", "pod": "api"}, ""},
		{"other pod same namespace", "k8s.logs", map[string]string{"namespace": "shop", "pod": "db-0", "previous": "true"}, ""},
		{"unknown tool", "k8s.exec", map[string]string{}, "unknown tool"},
		{"wrong platform", "docker.logs", map[string]string{"container": "x"}, "is for docker"},
		{"missing arg", "k8s.logs", map[string]string{"namespace": "shop"}, "missing argument"},
		{"unknown arg", "k8s.logs", map[string]string{"namespace": "shop", "pod": "api", "all": "1"}, "unknown argument"},
		{"bad pattern", "k8s.logs", map[string]string{"namespace": "shop", "pod": "API;rm"}, "invalid value"},
		{"bad bool", "k8s.logs", map[string]string{"namespace": "shop", "pod": "api", "previous": "yes"}, "invalid value"},
		{"other namespace", "k8s.logs", map[string]string{"namespace": "kube-system", "pod": "api"}, "outside this session's scope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := r.Validate(c.tool, c.args)
			if c.errPart == "" && err != nil || c.errPart != "" && (err == nil || !strings.Contains(err.Error(), c.errPart)) {
				t.Fatalf("err=%v want %q", err, c.errPart)
			}
		})
	}
}

func TestScopeLockDocker(t *testing.T) {
	var calls int
	ctr := Param{Name: "container", Required: true, Pattern: containerName}
	r := NewRegistry(gateway.New(), model.Target{Platform: model.PlatformDocker, Container: "web"},
		fakeTool("docker.logs", model.PlatformDocker, &calls, ctr))
	if _, err := r.Call(context.Background(), "docker.logs", map[string]string{"container": "billing-db"}); err == nil {
		t.Fatal("other container allowed")
	}
	if calls != 0 {
		t.Fatal("rejected request ran")
	}
	ev, err := r.Call(context.Background(), "docker.logs", map[string]string{"container": "web"})
	if err != nil || ev.ID != "E1" || calls != 1 {
		t.Fatalf("valid call failed: %v", err)
	}
}

func TestScopeLockFile(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "deploy.yaml")
	os.WriteFile(mine, []byte("image: app:1\npassword: file-secret-1\n"), 0o600)
	r := NewRegistry(gateway.New(), model.Target{Platform: model.PlatformK8s, Namespace: "a", Pod: "b", File: mine}, FileTool())
	if _, err := r.Call(context.Background(), "local.file", map[string]string{"path": "/etc/passwd"}); err == nil {
		t.Fatal("read outside scope allowed")
	}
	ev, err := r.Call(context.Background(), "local.file", map[string]string{"path": mine})
	if err != nil || strings.Contains(ev.Text, "file-secret-1") || !strings.Contains(ev.Text, "image: app:1") {
		t.Fatalf("file tool: %v %q", err, ev.Text)
	}
}

func TestToolErrorBecomesEvidence(t *testing.T) {
	tool := Tool{Name: "docker.events", Platform: model.PlatformDocker, Params: []Param{{Name: "container", Required: true, Pattern: containerName}},
		Run: func(context.Context, map[string]string) (string, error) {
			return "", errors.New("daemon said token=abc-secret-99")
		}}
	r := NewRegistry(gateway.New(), model.Target{Platform: model.PlatformDocker, Container: "web"}, tool)
	ev, err := r.Call(context.Background(), "docker.events", map[string]string{"container": "web"})
	if err != nil || !strings.HasPrefix(ev.Text, "tool error:") || strings.Contains(ev.Text, "abc-secret-99") {
		t.Fatalf("got %v %q", err, ev.Text)
	}
}

func TestReplayed(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "recorded"), 0o755)
	os.WriteFile(filepath.Join(dir, "recorded", "index.json"), []byte(`[{"tool":"docker.logs","args":{"container":"web"},"file":"logs.txt"}]`), 0o644)
	os.WriteFile(filepath.Join(dir, "recorded", "logs.txt"), []byte("Killed"), 0o644)
	recs, err := LoadRecordings(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(gateway.New(), model.Target{Platform: model.PlatformDocker, Container: "web"}, Replayed(DockerSpecs(), dir, recs)...)
	ev, _ := r.Call(context.Background(), "docker.logs", map[string]string{"container": "web"})
	if ev.Text != "Killed" {
		t.Fatalf("got %q", ev.Text)
	}
	ev, _ = r.Call(context.Background(), "docker.events", map[string]string{"container": "web"})
	if !strings.Contains(ev.Text, "no recorded output") {
		t.Fatalf("got %q", ev.Text)
	}
}
