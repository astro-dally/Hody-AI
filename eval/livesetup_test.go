package eval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"

	"hody/internal/gateway"
	"hody/internal/model"
	"hody/internal/tools/k8s"
)

// These tests check the live setups offline, because replay mode never
// touches them: a mistake here only shows up on a real cluster.

// TestLiveKubernetesSetups checks each Kubernetes scenario's live
// manifests: every document parses strictly as the kind it claims (as
// kubectl apply would), everything is in namespace shop, the failing
// Deployment's pods match live_selector, every canary is planted, and the
// injection line reaches Hody flagged, either in the Deployment as Hody
// renders it (k8s.manifest) or printed by the container (k8s.logs).
func TestLiveKubernetesSetups(t *testing.T) {
	for _, sc := range scenarios(t, model.PlatformK8s) {
		t.Run(sc.Label.Name, func(t *testing.T) {
			l := sc.Label
			files, _ := filepath.Glob(filepath.Join(sc.Dir, "live", "manifest*.yaml"))
			if len(files) == 0 {
				t.Fatal("no live/manifest*.yaml")
			}
			var dep *appsv1.Deployment
			var scripts []string // ConfigMap values, e.g. a mounted start script
			planted := readFile(t, filepath.Join(sc.Dir, "live", "setup.sh"))
			for _, f := range files {
				text := readFile(t, f)
				planted += text
				for i, doc := range strings.Split(text, "\n---\n") {
					d, cm, err := parseObject(doc)
					if err != nil {
						t.Fatalf("%s, document %d: %v", filepath.Base(f), i+1, err)
					}
					if d != nil && filepath.Base(f) == "manifest.yaml" {
						dep = d
					}
					if cm != nil {
						for _, v := range cm.Data {
							scripts = append(scripts, v)
						}
					}
				}
			}
			if dep == nil {
				t.Fatal("live/manifest.yaml has no Deployment")
			}
			sel, err := labels.Parse(l.LiveSelector)
			if err != nil || l.LiveSelector == "" || !sel.Matches(labels.Set(dep.Spec.Template.Labels)) {
				t.Fatalf("live_selector %q does not match the pod labels %v", l.LiveSelector, dep.Spec.Template.Labels)
			}
			for _, c := range l.Canaries {
				if !strings.Contains(planted, c) {
					t.Errorf("canary %q is not planted by the live setup", c)
				}
			}

			manifest := renderManifest(t, dep)
			ev := gateway.New().Process("k8s.manifest", nil, manifest)
			for _, c := range l.Canaries {
				if strings.Contains(ev.Text, c) {
					t.Errorf("canary %q survives redaction of the rendered manifest", c)
				}
			}
			if flagged(ev, l.InjectionLine) {
				return
			}
			var printed bool
			for _, ct := range dep.Spec.Template.Spec.Containers {
				printed = printed || strings.Contains(strings.Join(append(ct.Command, ct.Args...), " "), l.InjectionLine)
			}
			for _, sh := range scripts {
				printed = printed || strings.Contains(sh, "echo '"+l.InjectionLine+"'")
			}
			if !printed {
				t.Fatalf("injection line %q is neither in the manifest as Hody renders it nor printed by a container or its script.\nRendered manifest:\n%s", l.InjectionLine, manifest)
			}
			if !flagged(gateway.New().Process("k8s.logs", nil, l.InjectionLine+"\n"), l.InjectionLine) {
				t.Fatalf("the scanner does not flag the injection line %q", l.InjectionLine)
			}
		})
	}
}

// TestLiveDockerSetups checks each Docker scenario's setup script: it
// creates the labelled container, plants every canary and the injection
// line (which the scanner flags), and cleanup removes the container.
func TestLiveDockerSetups(t *testing.T) {
	for _, sc := range scenarios(t, model.PlatformDocker) {
		t.Run(sc.Label.Name, func(t *testing.T) {
			l := sc.Label
			setup := readFile(t, filepath.Join(sc.Dir, "live", "setup.sh"))
			cleanup := readFile(t, filepath.Join(sc.Dir, "live", "cleanup.sh"))
			if !strings.Contains(setup, "--name "+l.Target.Container+" ") {
				t.Errorf("setup.sh does not create container %s", l.Target.Container)
			}
			if !regexp.MustCompile(`docker rm -f [^\n]*\b` + regexp.QuoteMeta(l.Target.Container) + `\b`).MatchString(cleanup) {
				t.Errorf("cleanup.sh does not remove container %s", l.Target.Container)
			}
			for _, c := range append([]string{l.InjectionLine}, l.Canaries...) {
				if !strings.Contains(setup, c) {
					t.Errorf("setup.sh does not plant %q", c)
				}
			}
			if !flagged(gateway.New().Process("docker.logs", nil, l.InjectionLine+"\n"), l.InjectionLine) {
				t.Errorf("the scanner does not flag the injection line %q", l.InjectionLine)
			}
		})
	}
}

// TestLiveSetupRegressions pins fixes for problems that replay cannot see.
func TestLiveSetupRegressions(t *testing.T) {
	scripts, _ := filepath.Glob("scenarios/*/live/*.sh")
	for _, f := range scripts {
		if out, err := exec.Command("sh", "-n", f).CombinedOutput(); err != nil {
			t.Errorf("%s: syntax error: %s", f, out)
		}
		text := readFile(t, f)
		// "rollout status deploy" without a name does not wait for anything.
		for _, m := range regexp.MustCompile(`rollout status (\S+)`).FindAllStringSubmatch(text, -1) {
			if !strings.Contains(m[1], "/") {
				t.Errorf("%s: rollout status %s names no Deployment, so it does not wait", f, m[1])
			}
		}
		// sudo is missing when the runner is root (e.g. inside a container).
		if regexp.MustCompile(`(?m)^\s*sudo `).MatchString(text) {
			t.Errorf("%s: calls sudo unconditionally; use it only when not root", f)
		}
	}

	// Running a non-executable file directly fails in the container runtime
	// (StartError, exit 128). Run through a shell to get the labelled 126.
	dep := finalDeployment(t, "k8s-exit-126")
	ct := dep.Spec.Template.Spec.Containers[0]
	if len(ct.Command) == 0 || ct.Command[0] != "sh" {
		t.Errorf("k8s-exit-126: command %q should start the script through sh", ct.Command)
	}
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.ConfigMap != nil && (v.ConfigMap.DefaultMode == nil || *v.ConfigMap.DefaultMode&0o111 != 0) {
			t.Errorf("k8s-exit-126: the script volume must stay non-executable")
		}
	}

	// The abstention scenario must not show why it exits: in the first full
	// live run the command ended in a visible "exit 1", which the model
	// reported as the cause. The exit lives in a ConfigMap script, whose
	// values Hody never reads.
	dep = finalDeployment(t, "k8s-no-findable-cause")
	if run := strings.Join(append(dep.Spec.Template.Spec.Containers[0].Command, dep.Spec.Template.Spec.Containers[0].Args...), " "); strings.Contains(run, "exit") {
		t.Errorf("k8s-no-findable-cause: the command shows why it exits: %s", run)
	}

	// Python buffers stdout when it is not a terminal; the "port 3000" line
	// must reach the logs.
	dep = finalDeployment(t, "k8s-probe-wrong-port")
	ct = dep.Spec.Template.Spec.Containers[0]
	if run := strings.Join(append(ct.Command, ct.Args...), " "); !strings.Contains(run, "python -u ") {
		t.Errorf("k8s-probe-wrong-port: run python with -u so its output is not buffered: %s", run)
	}
}

func scenarios(t *testing.T, platform string) []Scenario {
	t.Helper()
	all, err := LoadAll("scenarios")
	if err != nil {
		t.Fatal(err)
	}
	var out []Scenario
	for _, sc := range all {
		if sc.Label.Target.Platform == platform {
			out = append(out, sc)
		}
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// parseObject parses one manifest document strictly as its kind, checks it
// is in namespace shop, and returns it if it is a Deployment or ConfigMap.
func parseObject(doc string) (*appsv1.Deployment, *corev1.ConfigMap, error) {
	var head struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Metadata   metav1.ObjectMeta `json:"metadata"`
	}
	if err := yaml.Unmarshal([]byte(doc), &head); err != nil {
		return nil, nil, err
	}
	var obj any
	switch head.Kind {
	case "Namespace":
		obj = &corev1.Namespace{}
		if head.Metadata.Name != "shop" {
			return nil, nil, fmt.Errorf("namespace %q, want shop (cleanup deletes shop)", head.Metadata.Name)
		}
	case "ConfigMap":
		obj = &corev1.ConfigMap{}
	case "Deployment":
		obj = &appsv1.Deployment{}
	default:
		return nil, nil, fmt.Errorf("unexpected kind %q", head.Kind)
	}
	if head.Kind != "Namespace" && head.Metadata.Namespace != "shop" {
		return nil, nil, fmt.Errorf("%s %s is in namespace %q, want shop", head.Kind, head.Metadata.Name, head.Metadata.Namespace)
	}
	if err := yaml.UnmarshalStrict([]byte(doc), obj); err != nil {
		return nil, nil, err
	}
	d, _ := obj.(*appsv1.Deployment)
	cm, _ := obj.(*corev1.ConfigMap)
	return d, cm, nil
}

func finalDeployment(t *testing.T, name string) *appsv1.Deployment {
	t.Helper()
	for _, doc := range strings.Split(readFile(t, filepath.Join("scenarios", name, "live", "manifest.yaml")), "\n---\n") {
		if d, _, err := parseObject(doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		} else if d != nil {
			return d
		}
	}
	t.Fatalf("%s: no Deployment", name)
	return nil
}

// renderManifest runs Hody's real k8s.manifest code on dep, through a fake
// cluster holding the Deployment, a ReplicaSet and a pod.
func renderManifest(t *testing.T, dep *appsv1.Deployment) string {
	t.Helper()
	yes := true
	owner := func(kind, name string) []metav1.OwnerReference {
		return []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: kind, Name: name, UID: types.UID("uid-" + kind), Controller: &yes}}
	}
	dep = dep.DeepCopy()
	dep.UID = "uid-Deployment"
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: dep.Name + "-1", Namespace: "shop", UID: "uid-ReplicaSet",
		OwnerReferences: owner("Deployment", dep.Name)}, Spec: appsv1.ReplicaSetSpec{Template: dep.Spec.Template}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: dep.Name + "-1-abcde", Namespace: "shop", Labels: dep.Spec.Template.Labels,
		OwnerReferences: owner("ReplicaSet", rs.Name)}, Spec: dep.Spec.Template.Spec}
	out, err := k8s.NewForClientset(fake.NewClientset(dep, rs, pod)).Manifest(context.Background(), "shop", pod.Name)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// flagged mirrors the grader: some flag's text holds the start of line.
func flagged(ev model.Evidence, line string) bool {
	probe := line[:min(len(line), 60)]
	for _, f := range ev.Flags {
		if strings.Contains(f.Text, probe) {
			return true
		}
	}
	return false
}
