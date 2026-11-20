package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Services lists Services with selectors, ports and endpoint readiness.
// Endpoints are read from EndpointSlices, the current Endpoints API.
func (c *Client) Services(ctx context.Context, ns string) (string, error) {
	svcs, err := c.cs.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	slices, err := c.cs.DiscoveryV1().EndpointSlices(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	ready, notReady := map[string]int{}, map[string]int{}
	for _, s := range slices.Items {
		svc := s.Labels[discoveryv1.LabelServiceName]
		for _, ep := range s.Endpoints {
			if ep.Conditions.Ready == nil || *ep.Conditions.Ready {
				ready[svc] += len(ep.Addresses)
			} else {
				notReady[svc] += len(ep.Addresses)
			}
		}
	}
	var b strings.Builder
	for _, s := range svcs.Items {
		var ports []string
		for _, p := range s.Spec.Ports {
			ports = append(ports, fmt.Sprintf("%d->%s/%s", p.Port, p.TargetPort.String(), p.Protocol))
		}
		fmt.Fprintf(&b, "service %s type=%s selector=%s ports=%s readyEndpoints=%d notReadyEndpoints=%d\n",
			s.Name, s.Spec.Type, labels(s.Spec.Selector), strings.Join(ports, ","), ready[s.Name], notReady[s.Name])
	}
	if b.Len() == 0 {
		return "no services in namespace " + ns, nil
	}
	return b.String(), nil
}

func labels(m map[string]string) string {
	var out []string
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// NodeConditions describes the node the pod is scheduled on.
func (c *Client) NodeConditions(ctx context.Context, ns, pod string) (string, error) {
	p, err := c.pod(ctx, ns, pod)
	if err != nil {
		return "", err
	}
	if p.Spec.NodeName == "" {
		return "pod is not scheduled to a node yet", nil
	}
	n, err := c.cs.CoreV1().Nodes().Get(ctx, p.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }
	w("node %s unschedulable=%t allocatable=%s", n.Name, n.Spec.Unschedulable, resources(n.Status.Allocatable))
	for _, cond := range n.Status.Conditions {
		w("condition: %s=%s reason=%s message=%q", cond.Type, cond.Status, cond.Reason, cond.Message)
	}
	for _, t := range n.Spec.Taints {
		w("taint: %s=%s:%s", t.Key, t.Value, t.Effect)
	}
	return b.String(), nil
}

// Quotas lists ResourceQuotas with hard limits and usage.
func (c *Client) Quotas(ctx context.Context, ns string) (string, error) {
	list, err := c.cs.CoreV1().ResourceQuotas(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, q := range list.Items {
		fmt.Fprintf(&b, "quota %s hard=%s used=%s\n", q.Name, resources(q.Status.Hard), resources(q.Status.Used))
	}
	if b.Len() == 0 {
		return "no resource quotas in namespace " + ns, nil
	}
	return b.String(), nil
}

// LineDiff returns the changed lines between a and b, marked "-" and "+",
// with one line of context. It uses a longest-common-subsequence table,
// which is fine for pod templates of a few hundred lines.
func LineDiff(a, b string) string {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	lcs := make([][]int, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type op struct {
		mark byte
		text string
	}
	var ops []op
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			ops, i, j = append(ops, op{' ', x[i]}), i+1, j+1
		case i < len(x) && (j == len(y) || lcs[i+1][j] >= lcs[i][j+1]):
			ops, i = append(ops, op{'-', x[i]}), i+1
		default:
			ops, j = append(ops, op{'+', y[j]}), j+1
		}
	}
	var out strings.Builder
	for k, o := range ops {
		near := o.mark != ' ' || (k > 0 && ops[k-1].mark != ' ') || (k+1 < len(ops) && ops[k+1].mark != ' ')
		if near {
			fmt.Fprintf(&out, "%c %s\n", o.mark, o.text)
		}
	}
	return out.String()
}
