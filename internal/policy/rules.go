// Package policy checks proposed fixes for safety. It applies a diff to an
// in-memory copy of the manifest, runs every rule on the original and the
// patched text, and reports a finding for each rule the patch makes worse.
// Rules match text, so they work on Kubernetes YAML, compose files,
// Dockerfiles and shell commands alike. Each finding explains the risk and
// a safer alternative in plain language, because Hody's users are learning.
package policy

import (
	"regexp"

	"hody/internal/model"
)

// Rule counts occurrences of something in a manifest. For most rules more
// occurrences are worse; for "removed" rules fewer are worse.
type Rule struct {
	Name        string
	Severity    string // model.VerdictBlock or model.VerdictWarn
	FewerIsBad  bool
	Risk        string
	Alternative string
	patterns    []*regexp.Regexp
}

func (r Rule) count(text string) int {
	n := 0
	for _, re := range r.patterns {
		n += len(re.FindAllStringIndex(text, -1))
	}
	return n
}

func res(exprs ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(exprs))
	for i, e := range exprs {
		out[i] = regexp.MustCompile(e)
	}
	return out
}

const secretKey = `[A-Za-z0-9_.\-]*(?i:password|passwd|secret|token|api_?key|access_?key|private_?key|credential)[A-Za-z0-9_.\-]*`

// Rules are all fix policy rules.
var Rules = []Rule{
	{"privileged-mode", model.VerdictBlock, false,
		"Privileged mode gives the container almost full control of the host; a bug or attacker inside it can take over the machine.",
		"Find the specific permission the app needs and grant only that, or fix file ownership instead.",
		res(`(?im)^\s*-?\s*"?privileged"?\s*:\s*"?true`, `--privileged\b`)},
	{"runs-as-root", model.VerdictBlock, false,
		"Running as root means anyone who breaks into the app controls everything in the container and has an easier path to the host.",
		"Keep a non-root user and fix ownership of the files or volume instead (for example fsGroup, or chown in the image).",
		res(`(?im)^\s*-?\s*"?(user|runAsUser)"?\s*:\s*"?(root|0)"?\s*(#.*)?$`, `(?im)runAsNonRoot\s*:\s*false`,
			`(?im)allowPrivilegeEscalation\s*:\s*true`, `(?m)^\s*USER\s+(root|0)\b`, `--user[ =](root|0)\b`)},
	{"host-namespaces", model.VerdictBlock, false,
		"Sharing the host's network or process namespace removes the isolation between the container and the machine.",
		"Publish the needed port or use a Service instead of host networking; never share the host PID namespace for an app.",
		res(`(?im)^\s*-?\s*(hostNetwork|hostPID|hostIPC)\s*:\s*true`, `(?im)network_mode\s*:\s*"?host`, `(?im)^\s*(pid|ipc)\s*:\s*"?host`, `--(network|net|pid|ipc)[ =]host\b`)},
	{"host-path-mount", model.VerdictBlock, false,
		"Mounting a host directory lets the container read or change files on the machine itself.",
		"Use a named volume, emptyDir or a PersistentVolumeClaim instead of a host path.",
		res(`(?m)^\s*hostPath\s*:`, `(?m)^\s*-\s*"?/[^:\s"]*:/`, `(?m)\s-v\s+/`)},
	{"added-capabilities", model.VerdictBlock, false,
		"Extra Linux capabilities give the container powers normally reserved for root, such as changing the network or loading kernel modules.",
		"Drop all capabilities and solve the underlying problem; if one is truly needed, add only that one after review.",
		res(`(?m)^\s*(add|cap_add)\s*:`, `--cap-add\b`)},
	{"removed-resource-limits", model.VerdictWarn, true,
		"Without limits, one container can use all memory or CPU on the machine and take other workloads down with it.",
		"Keep the limits and raise them to a measured value instead of deleting them.",
		res(`(?m)^\s*limits\s*:`, `(?m)^\s*mem_limit\s*:`, `(?m)^\s*cpus\s*:`, `--memory[ =]`)},
	{"removed-probes", model.VerdictWarn, true,
		"Without health probes the platform cannot notice a stuck app or keep traffic away from it.",
		"Keep the probe and correct its port, path or timing.",
		res(`(?m)^\s*(livenessProbe|readinessProbe|startupProbe|healthcheck)\s*:`, `(?m)^\s*HEALTHCHECK\s`)},
	{"latest-tag", model.VerdictWarn, false,
		"The latest tag can change at any time, so the same manifest may run different code tomorrow and rollbacks become guesswork.",
		"Pin a specific version tag, or better, an image digest.",
		res(`(?im)(image\s*:\s*"?|^\s*FROM\s+)[^\s"@]+:latest\b`, `(?im)^\s*-?\s*image\s*:\s*"?[a-z0-9][a-z0-9./_-]*"?\s*$`)},
	{"inline-secret", model.VerdictBlock, false,
		"A secret written into a manifest ends up in version control, logs and backups, where many people can read it.",
		"Store the value in a Secret (or your secret manager) and reference it, for example with secretKeyRef.",
		res(`(?m)^[ \t]*-?[ \t]*"?`+secretKey+`"?[ \t]*[:=][ \t]*"?[^\s"{$]`, `(?m)name\s*:\s*"?`+secretKey+`"?\s*\n\s*value\s*:`, `\[SECRET_\d+\]`)},
	{"broad-permissions", model.VerdictBlock, false,
		"World-writable files let any process or user change them, including code that will be executed.",
		"Give the file only the permission it needs, for example chmod 755 for a script or 0755 as the ConfigMap defaultMode.",
		res(`chmod\s+(-R\s+)?(0?777|0?666|a\+rwx|ugo\+rwx|o\+w)\b`, `(?im)(defaultMode|mode)\s*:\s*0?(777|666|511|438)\b`)},
	{"tls-verification-disabled", model.VerdictBlock, false,
		"Turning off certificate checks lets anyone on the network impersonate the server and read or change the traffic.",
		"Install the right CA certificate in the image or mount it, and keep verification on.",
		res(`(?i)insecure-skip-tls-verify`, `(?i)insecureSkipVerify\s*:\s*true`, `(?i)NODE_TLS_REJECT_UNAUTHORIZED\s*[=:]\s*"?0`,
			`(?i)\bverify\s*=\s*False\b`, `(?i)sslmode=disable`, `(?i)PYTHONHTTPSVERIFY\s*[=:]\s*"?0`, `\bcurl\b[^\n]*\s(-k|--insecure)\b`, `(?i)tls_?verify\s*[:=]\s*"?false`)},
}

// Check returns a finding for every rule the patch makes worse.
func Check(original, patched string) []model.Finding {
	var out []model.Finding
	for _, r := range Rules {
		before, after := r.count(original), r.count(patched)
		if after > before && !r.FewerIsBad || after < before && r.FewerIsBad {
			out = append(out, model.Finding{Rule: r.Name, Severity: r.Severity, Risk: r.Risk, Alternative: r.Alternative})
		}
	}
	return out
}

// NotApplied is the finding for a diff that does not apply.
func NotApplied(reason string) model.Finding {
	return model.Finding{Rule: "patch-does-not-apply", Severity: model.VerdictBlock,
		Risk:        "The proposed diff does not match the manifest (" + reason + "), so nobody can tell what it would really change.",
		Alternative: "Ask for the diagnosis again, or make the change by hand after reading the explanation."}
}

// Verdict is the strictest severity among findings, or approve.
func Verdict(findings []model.Finding) string {
	v := model.VerdictApprove
	for _, f := range findings {
		v = Stricter(v, f.Severity)
	}
	return v
}

// Stricter returns the stricter of two verdicts. Unknown values count as
// block, so a malformed verdict can never loosen a review.
func Stricter(a, b string) string {
	rank := func(v string) int {
		switch v {
		case model.VerdictApprove:
			return 0
		case model.VerdictWarn:
			return 1
		}
		return 2
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}
