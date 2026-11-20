package docker

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"hody/internal/tools/docker/dockertest"
)

func TestReadOnlyRefusesNonGET(t *testing.T) {
	f := dockertest.NewFakeAPI()
	defer f.Close()
	c := NewForTest(f.URL)
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT"} {
		req, _ := http.NewRequest(m, f.URL+"/containers/web/json", nil)
		if _, err := c.HTTPClient().Do(req); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("%s was not refused: %v", m, err)
		}
	}
	if len(f.Methods) != 0 {
		t.Fatalf("refused requests reached the daemon: %v", f.Methods)
	}
}

func TestReadOnlyPathAllowlist(t *testing.T) {
	cases := []struct {
		path string
		ok   bool
	}{
		{"/containers/json", true},
		{"/v1.47/containers/web/json", true},
		{"/containers/web/logs", true},
		{"/events", true},
		{"/images/library/nginx:1.27/json", true},
		{"/containers/web/kill", false},
		{"/containers/create", false},
		{"/containers/web/exec", false},
		{"/containers/web/archive", false},
		{"/images/create", false},
		{"/containers/web/../../images/x/json", false},
		{"/secrets", false},
		{"/info", false},
		{"/exec/abc/json", false},
	}
	f := dockertest.NewFakeAPI()
	defer f.Close()
	c := NewForTest(f.URL)
	for _, cs := range cases {
		req, _ := http.NewRequest("GET", f.URL+cs.path, nil)
		_, err := c.HTTPClient().Do(req)
		refused := err != nil && strings.Contains(err.Error(), "allowlist")
		if refused == cs.ok {
			t.Errorf("%s: refused=%v want allowed=%v", cs.path, refused, cs.ok)
		}
	}
}

func TestClientAgainstFake(t *testing.T) {
	f := dockertest.NewFakeAPI()
	defer f.Close()
	c := NewForTest(f.URL)
	ctx := context.Background()
	ct, err := c.Inspect(ctx, "web")
	if err != nil || !ct.State.OOMKilled || ct.State.ExitCode != 137 {
		t.Fatalf("inspect: %+v %v", ct.State, err)
	}
	text := FormatInspect(ct)
	for _, want := range []string{"exitCode=137 oomKilled=true", "env: DB_PASSWORD=fake-db-pass-123", "portBinding: :8080 -> 8080/tcp"} {
		if !strings.Contains(text, want) {
			t.Errorf("inspect text lacks %q:\n%s", want, text)
		}
	}
	logs, err := c.Logs(ctx, "web", 50)
	if err != nil || logs != "starting\nstderr: Killed\n" {
		t.Fatalf("logs %q %v", logs, err)
	}
	ev, err := c.Events(ctx, "web", time.Hour)
	if err != nil || len(ev) != 2 || !strings.Contains(FormatEvents(ev), "die exitCode=137") {
		t.Fatalf("events %+v %v", ev, err)
	}
	img, err := c.InspectImage(ctx, ct.Config.Image)
	if err != nil || img.Config.User != "node" {
		t.Fatalf("image %+v %v", img, err)
	}
	if _, err := c.ListContainers(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Inspect(ctx, "missing"); err == nil {
		t.Fatal("expected 404 error")
	}
	for _, m := range f.Methods {
		if m != "GET" {
			t.Fatalf("non-GET reached the daemon: %s", m)
		}
	}
	compose := Compose(ct)
	for _, want := range []string{"  web:", "image: shop/web:1.4", "mem_limit: 67108864", `- "8080:8080"`, "- /srv/data:/data"} {
		if !strings.Contains(compose, want) {
			t.Errorf("compose lacks %q:\n%s", want, compose)
		}
	}
}

func TestDemux(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"multiplexed", append(dockertest.Frame(1, "a\n"), dockertest.Frame(2, "b\nc\n")...), "a\nstderr: b\nstderr: c\n"},
		{"tty raw stream", []byte("plain output\n"), "plain output\n"},
		{"truncated frame kept raw", dockertest.Frame(1, "abc")[:9], string(dockertest.Frame(1, "abc")[:9])},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		if got := Demux(c.in); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
