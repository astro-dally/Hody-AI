package k8s

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func state(s corev1.ContainerState) string {
	switch {
	case s.Waiting != nil:
		return fmt.Sprintf("waiting reason=%s message=%q", s.Waiting.Reason, s.Waiting.Message)
	case s.Running != nil:
		return "running since " + s.Running.StartedAt.UTC().Format("2006-01-02T15:04:05Z")
	case s.Terminated != nil:
		t := s.Terminated
		return fmt.Sprintf("terminated reason=%s exitCode=%d message=%q finishedAt=%s", t.Reason, t.ExitCode, t.Message, t.FinishedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	return "unknown"
}

func resources(r corev1.ResourceList) string {
	if len(r) == 0 {
		return "{}"
	}
	var parts []string
	for name, q := range r {
		parts = append(parts, string(name)+":"+q.String())
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, " ") + "}"
}

// envLine shows literal values (the gateway redacts secret-looking ones)
// and references by name only; Secret values are never fetched.
func envLine(e corev1.EnvVar) string {
	if e.ValueFrom == nil {
		return e.Name + "=" + e.Value
	}
	switch v := e.ValueFrom; {
	case v.ConfigMapKeyRef != nil:
		return fmt.Sprintf("%s from configMapKeyRef %s key=%s", e.Name, v.ConfigMapKeyRef.Name, v.ConfigMapKeyRef.Key)
	case v.SecretKeyRef != nil:
		return fmt.Sprintf("%s from secretKeyRef %s key=%s (value not read)", e.Name, v.SecretKeyRef.Name, v.SecretKeyRef.Key)
	case v.FieldRef != nil:
		return fmt.Sprintf("%s from fieldRef %s", e.Name, v.FieldRef.FieldPath)
	}
	return e.Name + " from another source"
}

func volumeSource(v corev1.Volume) string {
	switch s := v.VolumeSource; {
	case s.ConfigMap != nil:
		return "configMap " + s.ConfigMap.Name
	case s.Secret != nil:
		return "secret " + s.Secret.SecretName + " (not read)"
	case s.PersistentVolumeClaim != nil:
		return "pvc " + s.PersistentVolumeClaim.ClaimName
	case s.HostPath != nil:
		return "hostPath " + s.HostPath.Path
	case s.EmptyDir != nil:
		return "emptyDir"
	}
	return "other"
}

func probe(p *corev1.Probe) string {
	var how string
	switch h := p.ProbeHandler; {
	case h.HTTPGet != nil:
		how = fmt.Sprintf("httpGet path=%s port=%s", h.HTTPGet.Path, h.HTTPGet.Port.String())
	case h.TCPSocket != nil:
		how = "tcpSocket port=" + h.TCPSocket.Port.String()
	case h.Exec != nil:
		how = fmt.Sprintf("exec %q", h.Exec.Command)
	case h.GRPC != nil:
		how = fmt.Sprintf("grpc port=%d", h.GRPC.Port)
	}
	return fmt.Sprintf("%s initialDelay=%ds period=%ds timeout=%ds failureThreshold=%d", how, p.InitialDelaySeconds, p.PeriodSeconds, p.TimeoutSeconds, p.FailureThreshold)
}

func ptr[T any](v *T) string {
	if v == nil {
		return "unset"
	}
	return fmt.Sprint(*v)
}
