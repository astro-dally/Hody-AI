package k8s

import (
	"context"
	"strconv"

	"hody/internal/tools"
)

// Tools binds the Kubernetes tool specs to a read-only client. The
// binding lives here so that package tools never imports client-go.
func Tools(c *Client) []tools.Tool {
	type nsName func(ctx context.Context, ns, name string) (string, error)
	two := func(f nsName, key string) tools.RunFunc {
		return func(ctx context.Context, a map[string]string) (string, error) { return f(ctx, a["namespace"], a[key]) }
	}
	one := func(f func(context.Context, string) (string, error)) tools.RunFunc {
		return func(ctx context.Context, a map[string]string) (string, error) { return f(ctx, a["namespace"]) }
	}
	return tools.WithRuns(tools.K8sSpecs(), map[string]tools.RunFunc{
		"k8s.pod_status":      two(c.PodStatus, "pod"),
		"k8s.events":          two(c.Events, "pod"),
		"k8s.deployment":      two(c.Deployment, "name"),
		"k8s.what_changed":    two(c.WhatChanged, "pod"),
		"k8s.configmap_keys":  two(c.ConfigMapKeys, "name"),
		"k8s.node_conditions": two(c.NodeConditions, "pod"),
		"k8s.manifest":        two(c.Manifest, "pod"),
		"k8s.services":        one(c.Services),
		"k8s.quotas":          one(c.Quotas),
		"k8s.logs": func(ctx context.Context, a map[string]string) (string, error) {
			tail := int64(300)
			if v, ok := a["tail"]; ok {
				tail, _ = strconv.ParseInt(v, 10, 64)
			}
			return c.Logs(ctx, a["namespace"], a["pod"], a["container"], a["previous"] == "true", tail)
		},
	})
}
