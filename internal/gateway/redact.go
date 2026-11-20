package gateway

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// minSecretLen is the shortest value treated as a secret. Shorter values
// are left alone because replacing them everywhere would destroy the text;
// this is a documented false negative.
const minSecretLen = 4

// keyValueRE finds `key=value`, `key: value` and `"key": "value"` pairs.
var keyValueRE = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.\-]*)"?[ \t]*[:=][ \t]*(?:"([^"\n]*)"|'([^'\n]*)'|([^\s"',;}&\]]+))`)

// yamlEnvRE finds Kubernetes-style `name: X` followed by `value: Y`.
var yamlEnvRE = regexp.MustCompile(`(?m)name:[ \t]*["']?([A-Za-z0-9_.\-]+)["']?[ \t]*\r?\n[ \t-]*value:[ \t]*(?:"([^"\n]*)"|'([^'\n]*)'|([^\n]*\S))`)

// jsonEnvRE finds `{"name":"X","value":"Y"}`.
var jsonEnvRE = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"\s*,\s*"value"\s*:\s*"([^"]*)"`)

// secretPattern is a well-known secret format; group is the capture group
// holding the secret (0 means the whole match).
type secretPattern struct {
	name  string
	re    *regexp.Regexp
	group int
}

var secretPatterns = []secretPattern{
	{"private-key-block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), 0},
	{"aws-access-key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), 0},
	{"github-token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{30,}\b`), 0},
	{"github-pat", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`), 0},
	{"slack-token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`), 0},
	{"google-api-key", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`), 0},
	{"stripe-key", regexp.MustCompile(`\b[rs]k_live_[0-9A-Za-z]{16,}\b`), 0},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{5,}\.eyJ[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]{5,}`), 0},
	{"bearer", regexp.MustCompile(`(?i)\bbearer[ \t]+([A-Za-z0-9._~+/=\-]{8,})`), 1},
	{"basic-auth", regexp.MustCompile(`(?i)\bbasic[ \t]+([A-Za-z0-9+/]{8,}={0,2})`), 1},
	{"url-credentials", regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.\-]*://[^/\s:@'"]+:([^/\s@'"]+)@`), 1},
}

// benignValues are never secrets even under a secret-looking key.
var benignValues = map[string]bool{
	"true": true, "false": true, "null": true, "none": true, "nil": true,
	"yes": true, "no": true, "required": true, "optional": true, "redacted": true,
	"bearer": true, "basic": true, "digest": true,
}

// benignLastWord marks keys whose value is a reference or setting, not a
// secret: secretName, passwordFile, tokenPath, secretKeyRef, keyId, and
// error names such as Python's KeyError.
var benignLastWord = map[string]bool{
	"name": true, "names": true, "ref": true, "file": true, "path": true,
	"dir": true, "mode": true, "type": true, "id": true, "length": true,
	"len": true, "count": true, "enabled": true, "required": true,
	"expiry": true, "ttl": true, "url": true, "endpoint": true, "header": true,
	"error": true, "errors": true, "exception": true, "missing": true,
}

// isSecretKey reports whether a key name suggests that its value is secret.
func isSecretKey(key string) bool {
	words := splitWords(key)
	if len(words) == 0 || benignLastWord[words[len(words)-1]] {
		return false
	}
	joined := strings.Join(words, "")
	for _, w := range []string{"password", "passwd", "passphrase", "secret", "token", "apikey", "accesskey", "privatekey", "credential", "cookie"} {
		if strings.Contains(joined, w) {
			return true
		}
	}
	for _, w := range words {
		if w == "auth" || w == "authorization" || w == "pass" || w == "pw" {
			return true
		}
		// A bare "key" is too common (configMapKeyRef.key); require a
		// qualifier such as api_key or SIGNING_KEY.
		if w == "key" && len(words) > 1 {
			return true
		}
	}
	return false
}

// splitWords splits a key on punctuation and camelCase into lower-case words.
func splitWords(s string) []string {
	var words []string
	var cur []rune
	prevLower := false
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for _, r := range s {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
			prevLower = false
			continue
		case unicode.IsUpper(r) && prevLower:
			flush()
		}
		cur = append(cur, r)
		prevLower = unicode.IsLower(r) || unicode.IsDigit(r)
	}
	flush()
	return words
}

// plausibleValue filters out values that cannot be secrets.
func plausibleValue(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) < minSecretLen || benignValues[strings.ToLower(v)] {
		return false
	}
	if isPlaceholder(v) || strings.HasPrefix(v, "${") || strings.HasPrefix(v, "$(") || strings.Trim(v, "*x") == "" {
		return false
	}
	return true
}

var placeholderRE = regexp.MustCompile(`^\[SECRET_\d+\]$`)

func isPlaceholder(v string) bool {
	return placeholderRE.MatchString(v) || strings.HasPrefix(v, "[SECRET_")
}

func firstGroup(m []string) string {
	for _, g := range m {
		if g != "" {
			return g
		}
	}
	return ""
}

// findByKeyName returns values held under secret-looking key names.
func findByKeyName(text string) []string {
	var out []string
	// Matches may nest ("args: --db-password=x"), so resume each search at
	// the start of the previous value instead of after the whole match.
	for pos := 0; pos < len(text); {
		loc := keyValueRE.FindStringSubmatchIndex(text[pos:])
		if loc == nil {
			break
		}
		key := text[pos+loc[2] : pos+loc[3]]
		vStart, value := -1, ""
		for g := 2; g <= 4; g++ {
			if loc[2*g] >= 0 {
				vStart, value = loc[2*g], text[pos+loc[2*g]:pos+loc[2*g+1]]
				break
			}
		}
		if isSecretKey(key) && plausibleValue(value) {
			out = append(out, value)
		}
		pos += max(vStart, loc[3])
	}
	for _, re := range []*regexp.Regexp{yamlEnvRE, jsonEnvRE} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if v := strings.TrimSpace(firstGroup(m[2:])); isSecretKey(m[1]) && plausibleValue(v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// findByPattern returns values in well-known secret formats.
func findByPattern(text string) []string {
	var out []string
	for _, p := range secretPatterns {
		for _, m := range p.re.FindAllStringSubmatch(text, -1) {
			if v := m[p.group]; plausibleValue(v) {
				out = append(out, v)
			}
		}
	}
	return out
}

var tokenRE = regexp.MustCompile(`[A-Za-z0-9+/_\-]{20,}={0,2}`)
var hexLikeRE = regexp.MustCompile(`^[0-9a-fA-F\-]+$`)

// kindNameRE matches Kubernetes object references such as
// ReplicaSet/orders-5c9d8f7b6, whose generated suffixes look random.
var kindNameRE = regexp.MustCompile(`^[A-Z][A-Za-z]+/[a-z0-9][a-z0-9.\-]*$`)

// findByEntropy returns long random-looking strings. Exempt: anything made
// only of hex digits and dashes (image digests, container IDs, commit
// hashes, UUIDs), Kubernetes Kind/name references, and anything without upper case, lower case and digits
// (pod names, paths, words).
func findByEntropy(text string) []string {
	var out []string
	for _, tok := range tokenRE.FindAllString(text, -1) {
		if hexLikeRE.MatchString(tok) || kindNameRE.MatchString(tok) || !mixedClasses(tok) {
			continue
		}
		limit := math.Min(4.5, 0.85*math.Log2(float64(len(tok))))
		if shannon(tok) >= limit {
			out = append(out, tok)
		}
	}
	return out
}

func mixedClasses(s string) bool {
	var up, low, dig bool
	for _, r := range s {
		up = up || unicode.IsUpper(r)
		low = low || unicode.IsLower(r)
		dig = dig || unicode.IsDigit(r)
	}
	return up && low && dig
}

// shannon returns the entropy of s in bits per character.
func shannon(s string) float64 {
	counts := map[rune]int{}
	for _, r := range s {
		counts[r]++
	}
	n := float64(len(s))
	h := 0.0
	for _, c := range counts {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// redact replaces every secret in text with its session placeholder and
// returns the new text and the number of replacements. It learns new
// secrets from text first, then replaces every secret known so far, so a
// password seen once is hidden everywhere it appears later.
func (g *Gateway) redact(text string) (string, int) {
	for _, find := range []func(string) []string{findByPattern, findByKeyName, findByEntropy} {
		for _, v := range find(text) {
			g.placeholder(v)
		}
	}
	if len(g.secrets) == 0 {
		return text, 0
	}
	values := make([]string, 0, len(g.secrets))
	for v := range g.secrets {
		values = append(values, v)
	}
	sort.Slice(values, func(i, j int) bool {
		if len(values[i]) != len(values[j]) {
			return len(values[i]) > len(values[j])
		}
		return values[i] < values[j]
	})
	pairs := make([]string, 0, 2*len(values))
	count := 0
	for _, v := range values {
		pairs = append(pairs, v, g.secrets[v])
		count += strings.Count(text, v)
	}
	return strings.NewReplacer(pairs...).Replace(text), count
}

// placeholder returns the stable placeholder for value, creating it once.
func (g *Gateway) placeholder(value string) string {
	if p, ok := g.secrets[value]; ok {
		return p
	}
	g.nextSecret++
	p := "[SECRET_" + strconv.Itoa(g.nextSecret) + "]"
	g.secrets[value] = p
	return p
}
