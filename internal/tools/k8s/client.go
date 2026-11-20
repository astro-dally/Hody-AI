// Package k8s holds Hody's read-only Kubernetes calls. It uses only get,
// list and log requests through client-go and never reads Secret objects;
// a test scans this package's source to keep it that way. Output is plain
// text for the gateway.
package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps a clientset. Tests pass the client-go fake clientset.
type Client struct{ cs kubernetes.Interface }

// New loads the user's kubeconfig (explicit path, $KUBECONFIG or
// ~/.kube/config) and returns a client.
func New(kubeconfig string) (*Client, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfig
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}
	cfg.UserAgent = "hody (read-only)"
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{cs: cs}, nil
}

// NewForClientset wraps an existing clientset, such as the fake one.
func NewForClientset(cs kubernetes.Interface) *Client { return &Client{cs: cs} }

func (c *Client) pod(ctx context.Context, ns, name string) (*corev1.Pod, error) {
	return c.cs.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
}

// PodStatus renders a pod's status and the parts of its spec that matter.
func (c *Client) PodStatus(ctx context.Context, ns, name string) (string, error) {
	p, err := c.pod(ctx, ns, name)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w("pod: %s/%s phase=%s node=%s qosClass=%s", ns, p.Name, p.Status.Phase, p.Spec.NodeName, p.Status.QOSClass)
	if p.Status.Reason != "" || p.Status.Message != "" {
		w("status: reason=%s message=%q", p.Status.Reason, p.Status.Message)
	}
	for _, o := range p.OwnerReferences {
		w("owner: %s/%s", o.Kind, o.Name)
	}
	for _, cond := range p.Status.Conditions {
		w("condition: %s=%s reason=%s message=%q", cond.Type, cond.Status, cond.Reason, cond.Message)
	}
	statuses := map[string]corev1.ContainerStatus{}
	for _, s := range append(p.Status.InitContainerStatuses, p.Status.ContainerStatuses...) {
		statuses[s.Name] = s
	}
	for _, ct := range append(p.Spec.InitContainers, p.Spec.Containers...) {
		writeContainer(w, ct, statuses[ct.Name])
	}
	for _, v := range p.Spec.Volumes {
		w("volume: %s %s", v.Name, volumeSource(v))
	}
	if sc := p.Spec.SecurityContext; sc != nil {
		w("podSecurityContext: runAsUser=%s runAsNonRoot=%s fsGroup=%s", ptr(sc.RunAsUser), ptr(sc.RunAsNonRoot), ptr(sc.FSGroup))
	}
	return b.String(), nil
}

func writeContainer(w func(string, ...any), ct corev1.Container, st corev1.ContainerStatus) {
	w("container %s: image=%s ready=%t restartCount=%d", ct.Name, ct.Image, st.Ready, st.RestartCount)
	w("  state: %s", state(st.State))
	if st.LastTerminationState != (corev1.ContainerState{}) {
		w("  lastState: %s", state(st.LastTerminationState))
	}
	w("  command=%q args=%q", ct.Command, ct.Args)
	w("  resources: requests=%s limits=%s", resources(ct.Resources.Requests), resources(ct.Resources.Limits))
	for _, e := range ct.Env {
		w("  env: %s", envLine(e))
	}
	for _, ef := range ct.EnvFrom {
		if ef.ConfigMapRef != nil {
			w("  envFrom: configMap %s", ef.ConfigMapRef.Name)
		}
		if ef.SecretRef != nil {
			w("  envFrom: secret %s (not read)", ef.SecretRef.Name)
		}
	}
	for _, port := range ct.Ports {
		w("  port: %d/%s", port.ContainerPort, port.Protocol)
	}
	for _, m := range ct.VolumeMounts {
		w("  volumeMount: %s -> %s readOnly=%t", m.Name, m.MountPath, m.ReadOnly)
	}
	for i, p := range []*corev1.Probe{ct.LivenessProbe, ct.ReadinessProbe, ct.StartupProbe} {
		if p != nil {
			w("  %sProbe: %s", []string{"liveness", "readiness", "startup"}[i], probe(p))
		}
	}
	if sc := ct.SecurityContext; sc != nil {
		w("  securityContext: runAsUser=%s runAsNonRoot=%s privileged=%s readOnlyRootFilesystem=%s",
			ptr(sc.RunAsUser), ptr(sc.RunAsNonRoot), ptr(sc.Privileged), ptr(sc.ReadOnlyRootFilesystem))
	}
}

// Events lists events for one pod, or all events in the namespace when
// pod is empty, oldest first.
func (c *Client) Events(ctx context.Context, ns, pod string) (string, error) {
	opts := metav1.ListOptions{}
	if pod != "" {
		opts.FieldSelector = fields.OneTermEqualSelector("involvedObject.name", pod).String()
	}
	list, err := c.cs.CoreV1().Events(ns).List(ctx, opts)
	if err != nil {
		return "", err
	}
	items := list.Items
	sort.Slice(items, func(i, j int) bool { return eventTime(items[i]).Time.Before(eventTime(items[j]).Time) })
	var b strings.Builder
	for _, e := range items {
		if pod != "" && e.InvolvedObject.Name != pod {
			continue // the fake clientset ignores field selectors
		}
		fmt.Fprintf(&b, "%s %s %s %s/%s: %s (x%d)\n", eventTime(e).UTC().Format("2006-01-02T15:04:05Z"), e.Type, e.Reason,
			e.InvolvedObject.Kind, e.InvolvedObject.Name, strings.TrimSpace(e.Message), max(e.Count, 1))
	}
	if b.Len() == 0 {
		return "no events found (events expire after about an hour)", nil
	}
	return b.String(), nil
}

func eventTime(e corev1.Event) metav1.Time {
	switch {
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp
	case !e.EventTime.IsZero():
		return metav1.Time{Time: e.EventTime.Time}
	}
	return e.FirstTimestamp
}

// Logs returns container logs; container defaults to the first one.
func (c *Client) Logs(ctx context.Context, ns, pod, container string, previous bool, tail int64) (string, error) {
	if container == "" {
		p, err := c.pod(ctx, ns, pod)
		if err != nil {
			return "", err
		}
		if len(p.Spec.Containers) > 0 {
			container = p.Spec.Containers[0].Name
		}
	}
	opts := &corev1.PodLogOptions{Container: container, Previous: previous, TailLines: &tail, Timestamps: true}
	raw, err := c.cs.CoreV1().Pods(ns).GetLogs(pod, opts).DoRaw(ctx)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "(no log output)", nil
	}
	return string(raw), nil
}

// FindPod returns the name of the newest pod matching a label selector.
// Used by live evaluation, where pod names are generated.
func (c *Client) FindPod(ctx context.Context, ns, selector string) (string, error) {
	list, err := c.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", err
	}
	if len(list.Items) == 0 {
		return "", fmt.Errorf("no pod in %s matches %s", ns, selector)
	}
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].CreationTimestamp.After(list.Items[j].CreationTimestamp.Time)
	})
	return list.Items[0].Name, nil
}
