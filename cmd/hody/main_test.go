package main

import (
	"bytes"
	"flag"
	"slices"
	"strings"
	"testing"

	"hody/internal/model"
)

func TestParseInterleaved(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	ns := fs.String("n", "default", "")
	pos, err := parseInterleaved(fs, []string{"k8s", "pod/api", "-n", "shop"})
	if err != nil || !slices.Equal(pos, []string{"k8s", "pod/api"}) || *ns != "shop" {
		t.Fatalf("pos=%v ns=%s err=%v", pos, *ns, err)
	}
}

func TestParseTarget(t *testing.T) {
	cases := []struct {
		platform, name string
		want           model.Target
		ok             bool
	}{
		{"docker", "web", model.Target{Platform: "docker", Container: "web"}, true},
		{"k8s", "pod/api", model.Target{Platform: "k8s", Pod: "api", Namespace: "shop"}, true},
		{"k8s", "deployment/api", model.Target{}, false},
		{"vm", "x", model.Target{}, false},
	}
	for _, c := range cases {
		got, err := parseTarget(c.platform, c.name, diagnoseOpts{namespace: "shop"})
		if (err == nil) != c.ok || c.ok && got != c.want {
			t.Errorf("%s %s: %+v %v", c.platform, c.name, got, err)
		}
	}
}

func TestCheckLoopback(t *testing.T) {
	cases := map[string]bool{"127.0.0.1:8080": true, "127.0.0.1:0": true, "0.0.0.0:8080": false, "localhost:8080": false, "[::]:80": false, "8080": false, "192.168.1.5:8080": false}
	for addr, ok := range cases {
		if err := checkLoopback(addr); (err == nil) != ok {
			t.Errorf("%s: %v", addr, err)
		}
	}
}

func TestReplayEvalCommand(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"eval", "../../eval/scenarios", "--work", t.TempDir()}, &out, &out); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "12 of 12 scenarios passed all checks.") {
		t.Fatal(out.String())
	}
}
