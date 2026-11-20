package pipeline

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"hody/internal/model"
)

// triageRule is a known failure signature, matched by code on redacted
// evidence. No model is involved when a rule matches.
type triageRule struct {
	name, category, summary string
	re                      *regexp.Regexp
}

var triageRules = []triageRule{
	{"oom-killed", "oom_killed", "The container was killed for exceeding its memory limit (OOMKilled / exit code 137).",
		regexp.MustCompile(`(?i)(oomkilled[=: ]+"?true|reason[=: ]+"?OOMKilled|exit ?code[=: ]+"?137\b|\bcontainer oom\b)`)},
	{"exit-126", "command_not_executable", "The entrypoint or command exists but cannot be executed (exit code 126).",
		regexp.MustCompile(`(?i)(exit ?code[=: ]+"?126\b|\bexec [^ ]+: permission denied)`)},
	{"exit-127", "command_not_found", "The entrypoint or command was not found (exit code 127).",
		regexp.MustCompile(`(?i)(exit ?code[=: ]+"?127\b|executable file not found|command not found|exec: "[^"]+": not found)`)},
	{"image-pull", "image_pull", "The image cannot be pulled (ImagePullBackOff / ErrImagePull).",
		regexp.MustCompile(`(?i)(ImagePullBackOff|ErrImagePull|manifest unknown|pull access denied|failed to pull image|repository does not exist)`)},
	{"probe-failed", "probe_failure", "A liveness, readiness or health probe is failing.",
		regexp.MustCompile(`(?i)((liveness|readiness|startup) probe failed|health: status=unhealthy|health_status: unhealthy)`)},
	{"config-error", "config_reference", "The container config references something missing (CreateContainerConfigError).",
		regexp.MustCompile(`(?i)(CreateContainerConfigError|couldn't find key \S+ in ConfigMap|configmap "[^"]+" not found)`)},
	{"unschedulable", "unschedulable", "The pod is Pending because no node can run it.",
		regexp.MustCompile(`(?i)(FailedScheduling|reason=Unschedulable|\d+/\d+ nodes are available|Insufficient (cpu|memory))`)},
	{"port-in-use", "port_conflict", "The port the container needs is already in use.",
		regexp.MustCompile(`(?i)(address already in use|port is already allocated|EADDRINUSE)`)},
	{"mount-permission", "mount_permission", "The process cannot access a mounted path (permission denied).",
		regexp.MustCompile(`(?i)((open|mkdir|write|create|access|stat|chown|unlink|rename)\S* '?/\S*'?: permission denied|EACCES: permission denied, \w+ '/)`)},
	{"missing-env", "missing_env", "The application reports a required environment variable as missing.",
		regexp.MustCompile(`(?i)((env(ironment)?( variable)?|variable)\s+['"]?[A-Z][A-Z0-9_]+['"]?\s+(is\s+)?(not set|missing|required|undefined|empty)|KeyError: '[A-Z][A-Z0-9_]+'|missing required (environment|env|config))`)},
}

// matchRules returns one hypothesis per rule that matches any evidence.
// The matched text is not copied into the hypothesis: hypotheses appear
// outside the data delimiters in prompts, so they cite evidence IDs only.
func matchRules(evidence []model.Evidence) []model.Hypothesis {
	var out []model.Hypothesis
	for _, r := range triageRules {
		h := model.Hypothesis{Source: "rule:" + r.name, Category: r.category, Summary: r.summary}
		for _, ev := range evidence {
			for _, line := range strings.Split(ev.Text, "\n") {
				if r.re.MatchString(line) {
					if !slices.Contains(h.EvidenceIDs, ev.ID) {
						h.EvidenceIDs = append(h.EvidenceIDs, ev.ID)
					}
				}
			}
		}
		if len(h.EvidenceIDs) > 0 {
			out = append(out, h)
		}
	}
	return out
}

// triage applies the code rules and asks the model only if none match.
func (e *Engine) triage(ctx context.Context, s *model.Session) error {
	s.Hypotheses = matchRules(s.Evidence)
	if len(s.Hypotheses) > 0 {
		return nil
	}
	var a triageAnswer
	if err := e.ask(ctx, s, "triage", e.messages(triageTask, sessionContext(s)), &a); err != nil {
		return err
	}
	for _, h := range a.Hypotheses {
		valid, _ := splitCitations(s, h.EvidenceIDs)
		s.Hypotheses = append(s.Hypotheses, model.Hypothesis{Source: "model", Category: h.Category, Summary: h.Summary, EvidenceIDs: valid})
	}
	return nil
}
