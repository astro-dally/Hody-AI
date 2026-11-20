package tools

import "hody/internal/model"

// Request is one tool call by name and arguments.
type Request struct {
	Tool string
	Args map[string]string
}

// ManifestTools produce the file a fix diff targets, in order of preference.
var ManifestTools = []string{"local.file", "k8s.manifest", "docker.config"}

// FirstLook is the fixed collect set for a target.
func FirstLook(t model.Target) []Request {
	var reqs []Request
	if t.Platform == model.PlatformDocker {
		for _, name := range DockerFirstLook {
			reqs = append(reqs, Request{name, map[string]string{"container": t.Container}})
		}
	} else {
		pod := func(extra ...string) map[string]string {
			m := map[string]string{"namespace": t.Namespace, "pod": t.Pod}
			for i := 0; i+1 < len(extra); i += 2 {
				m[extra[i]] = extra[i+1]
			}
			return m
		}
		reqs = []Request{
			{"k8s.pod_status", pod()},
			{"k8s.events", pod()},
			{"k8s.logs", pod()},
			{"k8s.logs", pod("previous", "true")},
			{"k8s.what_changed", pod()},
			{"k8s.manifest", pod()},
		}
	}
	if t.File != "" {
		reqs = append(reqs, Request{"local.file", map[string]string{"path": t.File}})
	}
	return reqs
}

// Specs returns the tool specs for a platform plus the file tool.
func Specs(platform string) []Tool {
	if platform == model.PlatformDocker {
		return append(DockerSpecs(), FileSpec())
	}
	return append(K8sSpecs(), FileSpec())
}
