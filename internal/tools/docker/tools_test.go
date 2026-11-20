package docker

import (
	"context"
	"strings"
	"testing"

	"hody/internal/gateway"
	"hody/internal/model"
	"hody/internal/tools"
	"hody/internal/tools/docker/dockertest"
)

func TestDescribeShowsScope(t *testing.T) {
	r := tools.NewRegistry(gateway.New(), model.Target{Platform: model.PlatformDocker, Container: "web"}, Tools(NewForTest("http://unused"))...)
	d := r.Describe()
	if !strings.Contains(d, `docker.logs(container="web", tail?)`) {
		t.Fatalf("describe:\n%s", d)
	}
}

func TestDockerToolsThroughGateway(t *testing.T) {
	f := dockertest.NewFakeAPI()
	defer f.Close()
	r := tools.NewRegistry(gateway.New(), model.Target{Platform: model.PlatformDocker, Container: "web"}, Tools(NewForTest(f.URL))...)
	for _, name := range append(tools.DockerFirstLook, "docker.inspect_image", "docker.list_containers") {
		ev, err := r.Call(context.Background(), name, map[string]string{"container": "web"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(ev.Block, "fake-db-pass-123") {
			t.Fatalf("%s leaked a secret", name)
		}
		if strings.HasPrefix(ev.Text, "tool error") {
			t.Fatalf("%s failed: %s", name, ev.Text)
		}
	}
}
