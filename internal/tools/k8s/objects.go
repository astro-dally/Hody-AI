package k8s

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	goyaml "go.yaml.in/yaml/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const revisionKey = "deployment.kubernetes.io/revision"

// The gateway scans evidence line by line. The YAML encoder behind
// sigs.k8s.io/yaml folds strings longer than 80 columns onto a second line,
// which can split an injected phrase so that no single line matches a scan
// rule. Render every value on one line instead, as kubectl does.
func init() { goyaml.FutureLineWrap() }

// Deployment renders a Deployment's status and its ReplicaSets.
func (c *Client) Deployment(ctx context.Context, ns, name string) (string, error) {
	d, err := c.cs.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w("deployment %s/%s revision=%s replicas=%s updated=%d ready=%d available=%d unavailable=%d",
		ns, d.Name, d.Annotations[revisionKey], ptr(d.Spec.Replicas), d.Status.UpdatedReplicas, d.Status.ReadyReplicas, d.Status.AvailableReplicas, d.Status.UnavailableReplicas)
	for _, cond := range d.Status.Conditions {
		w("condition: %s=%s reason=%s message=%q", cond.Type, cond.Status, cond.Reason, cond.Message)
	}
	rss, err := c.ownedReplicaSets(ctx, d)
	if err != nil {
		return "", err
	}
	for _, rs := range rss {
		w("replicaset %s revision=%s replicas=%d ready=%d images=%s", rs.Name, rs.Annotations[revisionKey], rs.Status.Replicas, rs.Status.ReadyReplicas, images(rs.Spec.Template.Spec))
	}
	return b.String(), nil
}

// ownedReplicaSets lists the ReplicaSets a Deployment owns, newest first.
func (c *Client) ownedReplicaSets(ctx context.Context, d *appsv1.Deployment) ([]appsv1.ReplicaSet, error) {
	list, err := c.cs.AppsV1().ReplicaSets(d.Namespace).List(ctx, metav1.ListOptions{LabelSelector: metav1.FormatLabelSelector(d.Spec.Selector)})
	if err != nil {
		return nil, err
	}
	var out []appsv1.ReplicaSet
	for _, rs := range list.Items {
		if ref := metav1.GetControllerOf(&rs); ref != nil && ref.UID == d.UID {
			out = append(out, rs)
		}
	}
	sort.Slice(out, func(i, j int) bool { return revision(out[i]) > revision(out[j]) })
	return out, nil
}

func revision(rs appsv1.ReplicaSet) int {
	n, _ := strconv.Atoi(rs.Annotations[revisionKey])
	return n
}

func images(spec corev1.PodSpec) string {
	var out []string
	for _, ct := range spec.Containers {
		out = append(out, ct.Image)
	}
	return strings.Join(out, ",")
}

// owners walks pod -> ReplicaSet -> Deployment. Missing links are nil.
func (c *Client) owners(ctx context.Context, p *corev1.Pod) (*appsv1.ReplicaSet, *appsv1.Deployment, error) {
	ref := metav1.GetControllerOf(p)
	if ref == nil || ref.Kind != "ReplicaSet" {
		return nil, nil, nil
	}
	rs, err := c.cs.AppsV1().ReplicaSets(p.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	ref = metav1.GetControllerOf(rs)
	if ref == nil || ref.Kind != "Deployment" {
		return rs, nil, nil
	}
	d, err := c.cs.AppsV1().Deployments(p.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	return rs, d, err
}

// WhatChanged compares the pod template of the pod's ReplicaSet with the
// previous revision's.
func (c *Client) WhatChanged(ctx context.Context, ns, pod string) (string, error) {
	p, err := c.pod(ctx, ns, pod)
	if err != nil {
		return "", err
	}
	cur, d, err := c.owners(ctx, p)
	if err != nil {
		return "", err
	}
	if d == nil {
		return "pod is not owned by a Deployment; nothing to compare", nil
	}
	rss, err := c.ownedReplicaSets(ctx, d)
	if err != nil {
		return "", err
	}
	var prev *appsv1.ReplicaSet
	for i := range rss {
		if revision(rss[i]) < revision(*cur) && (prev == nil || revision(rss[i]) > revision(*prev)) {
			prev = &rss[i]
		}
	}
	if prev == nil {
		return fmt.Sprintf("replicaset %s (revision %d) has no previous revision to compare", cur.Name, revision(*cur)), nil
	}
	a, b := templateYAML(prev.Spec.Template), templateYAML(cur.Spec.Template)
	head := fmt.Sprintf("pod template changes from replicaset %s (revision %d) to %s (revision %d):\n",
		prev.Name, revision(*prev), cur.Name, revision(*cur))
	if a == b {
		return head + "(no differences)", nil
	}
	return head + LineDiff(a, b), nil
}

func templateYAML(t corev1.PodTemplateSpec) string {
	t = *t.DeepCopy()
	delete(t.Labels, "pod-template-hash")
	out, err := yaml.Marshal(t)
	if err != nil {
		return "error rendering template: " + err.Error()
	}
	return string(out)
}

// ConfigMapKeys lists key names and value sizes, never the values.
func (c *Client) ConfigMapKeys(ctx context.Context, ns, name string) (string, error) {
	cm, err := c.cs.CoreV1().ConfigMaps(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	var keys []string
	for k, v := range cm.Data {
		keys = append(keys, fmt.Sprintf("key %s (value hidden, %d bytes)", k, len(v)))
	}
	for k, v := range cm.BinaryData {
		keys = append(keys, fmt.Sprintf("key %s (binary, hidden, %d bytes)", k, len(v)))
	}
	sort.Strings(keys)
	return fmt.Sprintf("configmap %s/%s has %d keys:\n%s\n", ns, name, len(keys), strings.Join(keys, "\n")), nil
}

// Manifest renders the pod's owning Deployment, or the pod itself, as
// YAML with status and server-managed metadata removed.
func (c *Client) Manifest(ctx context.Context, ns, pod string) (string, error) {
	p, err := c.pod(ctx, ns, pod)
	if err != nil {
		return "", err
	}
	_, d, err := c.owners(ctx, p)
	if err != nil {
		return "", err
	}
	type meta struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Labels    map[string]string `json:"labels,omitempty"`
	}
	type object struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   meta   `json:"metadata"`
		Spec       any    `json:"spec"`
	}
	obj := object{"v1", "Pod", meta{p.Name, ns, p.Labels}, p.Spec}
	if d != nil {
		obj = object{"apps/v1", "Deployment", meta{d.Name, ns, d.Labels}, d.Spec}
	}
	out, err := yaml.Marshal(obj)
	return "# Generated by Hody from the live object. Hody never applies changes.\n" + string(out), err
}
