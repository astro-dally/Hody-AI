package main

import (
	"context"

	"hody/internal/tools"
	"hody/internal/tools/k8s"
)

// k8sTools connects to the cluster in the user's kubeconfig.
func k8sTools(kubeconfig string) ([]tools.Tool, error) {
	c, err := k8s.New(kubeconfig)
	if err != nil {
		return nil, err
	}
	return k8s.Tools(c), nil
}

// findPod returns a PodFinder for live evaluation.
func findPod(kubeconfig string) func(context.Context, string, string) (string, error) {
	return func(ctx context.Context, ns, selector string) (string, error) {
		c, err := k8s.New(kubeconfig)
		if err != nil {
			return "", err
		}
		return c.FindPod(ctx, ns, selector)
	}
}
