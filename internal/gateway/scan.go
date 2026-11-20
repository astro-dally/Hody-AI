package gateway

import (
	"regexp"
	"strings"

	"hody/internal/model"
)

// scanRule is one heuristic for lines that look like instructions aimed at
// an AI. Flagged lines are kept as quoted data, never removed.
type scanRule struct {
	name string
	re   *regexp.Regexp
}

var scanRules = []scanRule{
	{"override-instructions", regexp.MustCompile(`(?i)\b(ignore|disregard|forget|override|bypass)\b.{0,40}\b(previous|prior|above|earlier|all|any|your|system)\b.{0,40}\b(instructions?|rules|prompts?|guidelines|polic(y|ies))\b`)},
	{"role-marker", regexp.MustCompile(`(?i)(<\|?(im_start|im_end|system|assistant)\|?>|^(?:[\d\-:.TZ+]+\s+|stderr:\s+|\[?(?:INFO|WARN|WARNING|ERROR|DEBUG|NOTICE)\]?:?\s+){0,3}\W{0,4}(system|assistant|developer)\s*(prompt)?\s*[:\]>]|\[/?(system|assistant|inst)\]|###\s*(system|assistant|instruction))`)},
	{"addressed-to-ai", regexp.MustCompile(`(?i)(\b(dear|hey|hi|hello|attention|note to( the)?|to the)\s+(ai|assistant|llm|model|agent|hody|chatgpt|claude|gpt|bot|reviewer|critic)\b|\b(ai|llm|language model|assistant|hody|agent)\s*[,:]\s*(you|please|ignore|do|now|always|never|report|say)\b|\bif you are an? (ai|llm|language model|assistant|agent)\b|\byou are now\b)`)},
	{"claims-approval", regexp.MustCompile(`(?i)(\b(pre-?approved|already (been )?approved|has been approved|was approved|auto-?approved?)\b|\bapproved by (the )?(admin|administrator|user|security|owner|sre|team|ops)\b|\bno need to (ask|confirm|review|check|verify)\b|\b(authori[sz]ed|cleared) (by|for) (the )?(admin|security|owner|user)\b)`)},
	{"delimiter-imitation", regexp.MustCompile(`(?i)(<<<|>>>|\bHODY_DATA(_END)?\b|\b(BEGIN|END)[ _-]?(EVIDENCE|TOOL[ _-]?OUTPUT|UNTRUSTED|DATA)\b)`)},
	{"asks-for-link", regexp.MustCompile(`(?i)\b(include|add|insert|put|paste|embed|append|show|send)\b.{0,40}\b(this |the following )?(link|url|image|https?://\S+)`)},
	{"out-of-scope-request", regexp.MustCompile(`(?i)(\b(please|you (must|should|need to)|now|also|then)\b.{0,30}\b(read|get|list|fetch|access|inspect|dump|print|check)\b.{0,40}\b(secrets?|other namespaces?|namespace [a-z0-9-]+|kube-system|all namespaces|other containers?|container [a-z0-9_.-]+|credentials|env(ironment)? of)\b|\bkubectl (get|describe) secrets?\b)`)},
	{"asserted-diagnosis", regexp.MustCompile(`(?i)(\b(the )?(real |actual |true )?root cause (is|was)\b|\b(report|say|conclude|state|tell (the user|them))\b.{0,30}\b(that )?(the )?(cause|problem|issue|failure) (is|was)\b|\bmark (this|the fix|it) as (safe|approved)\b)`)},
	{"targets-critic", regexp.MustCompile(`(?i)\b(critic|reviewer|safety (check|review)|policy (check|engine))\b.{0,50}\b(approve|pass|allow|accept|ignore|skip|safe)\b`)},
}

// scan returns a flag for every line matching an injection heuristic.
// Line numbers are 1-based and refer to the full (redacted) text.
func scan(lines []string) []model.Flag {
	var flags []model.Flag
	for i, line := range lines {
		for _, r := range scanRules {
			if r.re.MatchString(line) {
				flags = append(flags, model.Flag{Line: i + 1, Rule: r.name, Text: clip(strings.TrimSpace(line), 240)})
				break
			}
		}
	}
	return flags
}

// clip shortens s to at most n bytes without splitting a UTF-8 rune.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n] + " …[line truncated]"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
