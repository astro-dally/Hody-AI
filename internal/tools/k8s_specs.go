package tools

import "hody/internal/model"

// K8sSpecs describes the Kubernetes tools without binding them to a
// cluster. Every tool is namespaced to the session; node conditions are
// read only for the node the session's pod runs on.
func K8sSpecs() []Tool {
	ns := Param{Name: "namespace", Required: true, Pattern: k8sName, Doc: "the session's namespace"}
	pod := Param{Name: "pod", Required: true, Pattern: k8sName, Doc: "a pod in the namespace"}
	name := Param{Name: "name", Required: true, Pattern: k8sName, Doc: "object name"}
	k := model.PlatformK8s
	return []Tool{
		{Name: "k8s.pod_status", Platform: k, Params: []Param{ns, pod}, Doc: "phase, conditions, container states and exit codes, images, env (literal values redacted), probes, resources, owner"},
		{Name: "k8s.events", Platform: k, Params: []Param{ns, {Name: "pod", Pattern: k8sName, Doc: "omit for all namespace events"}}, Doc: "events for one pod, or all events in the namespace"},
		{Name: "k8s.logs", Platform: k, Params: []Param{ns, pod, {Name: "container", Pattern: k8sName}, {Name: "previous", Pattern: boolean, Doc: "logs of the previous (crashed) container"}, {Name: "tail", Pattern: smallNumber}}, Doc: "container logs"},
		{Name: "k8s.deployment", Platform: k, Params: []Param{ns, name}, Doc: "Deployment status and its ReplicaSets"},
		{Name: "k8s.what_changed", Platform: k, Params: []Param{ns, pod}, Doc: "difference between the pod template of the pod's ReplicaSet and the previous ReplicaSet of the same Deployment"},
		{Name: "k8s.configmap_keys", Platform: k, Params: []Param{ns, name}, Doc: "key names of a ConfigMap (values are never read out)"},
		{Name: "k8s.services", Platform: k, Params: []Param{ns}, Doc: "Services in the namespace with selectors, ports and ready endpoint counts"},
		{Name: "k8s.node_conditions", Platform: k, Params: []Param{ns, pod}, Doc: "conditions, allocatable resources and taints of the node the pod is on"},
		{Name: "k8s.quotas", Platform: k, Params: []Param{ns}, Doc: "ResourceQuota hard limits and usage in the namespace"},
		{Name: "k8s.manifest", Platform: k, Params: []Param{ns, pod}, Doc: "the pod's owning Deployment (or the pod) as YAML; fixes are diffs against it"},
	}
}
