package pipeline

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"hody/internal/model"
)

// conclude asks for a root cause or an explicit abstention, then checks in
// code that the answer is grounded in the evidence:
//
//   - every cited evidence ID exists;
//   - a known category's signature (the triage rule Hody uses to recognise
//     that failure, e.g. exit code 126 for command_not_executable) appears
//     somewhere in the evidence, and some cited evidence shows it;
//   - if the faulty setting is named in the evidence, some cited evidence
//     names it.
//
// On any problem it asks once more, saying what is wrong. If the second
// answer still cites evidence that does not exist, or still names a known
// failure while the evidence shows no known failure at all, it is
// downgraded to insufficient evidence. When the evidence shows a different
// known failure, the answer may be the same cause under a neighbouring
// name, so it is kept with a warning. Missing citations alone never
// downgrade: such an answer is less well supported, not wrong, and Hody
// records a warning.
func (e *Engine) conclude(ctx context.Context, s *model.Session) error {
	parts := []string{sessionContext(s)}
	for attempt := 0; ; attempt++ {
		var a concludeAnswer
		if err := e.ask(ctx, s, "conclude", e.messages(concludeTask, parts...), &a); err != nil {
			return err
		}
		g := ground(s, a)
		if g.ok() || attempt == 1 {
			s.RootCause = g.result(a)
			s.Warnings = append(s.Warnings, g.warnings()...)
			detail := map[string]string{"outcome": s.RootCause.Outcome, "category": a.Category, "evidence": strings.Join(g.valid, ",")}
			if s.RootCause.Note != "" {
				detail["downgraded"] = s.RootCause.Note
			}
			e.audit(s, "conclusion", detail)
			return nil
		}
		parts = []string{sessionContext(s), g.note(s)}
	}
}

// grounding is what the checks found in one answer.
type grounding struct {
	valid, invalid []string           // cited IDs that exist, and those that do not
	unsupported    *triageRule        // a known category whose signature no evidence shows
	seen           []model.Hypothesis // the known failures the evidence does show, if any
	symptomIn      []string           // evidence showing the category's signature, none of it cited
	setting        string             // the faulty setting's name, if no cited evidence names it
	settingIn      []string           // the evidence that does name it
}

func ground(s *model.Session, a concludeAnswer) grounding {
	var g grounding
	g.valid, g.invalid = splitCitations(s, a.EvidenceIDs)
	if a.Outcome != model.OutcomeRootCause {
		return g
	}
	if rule := ruleFor(a.Category); rule != nil {
		shown := signatureEvidence(s.Evidence, a.Category)
		switch {
		case len(shown) == 0:
			g.unsupported, g.seen = rule, matchRules(s.Evidence)
		case !overlaps(shown, g.valid):
			g.symptomIn = shown
		}
	}
	if name := settingName(a.Field); name != "" {
		if in := evidenceNaming(s.Evidence, name); len(in) > 0 && !overlaps(in, g.valid) {
			g.setting, g.settingIn = name, in
		}
	}
	return g
}

func (g grounding) ok() bool {
	return len(g.invalid) == 0 && g.unsupported == nil && g.symptomIn == nil && g.setting == ""
}

// result is the root cause to keep, downgraded if a hard problem remains.
func (g grounding) result(a concludeAnswer) *model.RootCause {
	rc := &model.RootCause{Outcome: a.Outcome, Symptom: a.Symptom, Category: a.Category, Summary: a.Summary,
		Object: a.Object, Field: a.Field, EvidenceIDs: g.valid, NextChecks: a.NextChecks}
	switch {
	case len(g.invalid) > 0:
		rc.Note = fmt.Sprintf("Downgraded by Hody: the answer cited evidence that does not exist (%s), twice.", strings.Join(g.invalid, ", "))
	case g.unsupported != nil && len(g.seen) == 0:
		rc.Note = fmt.Sprintf("Downgraded by Hody: the answer named %s, but nothing in the evidence shows that failure (%s), twice. Treat the suggestion above as unconfirmed.",
			a.Category, g.unsupported.summary)
	default:
		return rc
	}
	rc.Outcome = model.OutcomeInsufficient
	if len(rc.NextChecks) == 0 {
		rc.NextChecks = []string{"Review the evidence by hand; the model's conclusion could not be verified."}
	}
	return rc
}

// downgraded reports whether the answer is not kept as a root cause.
func (g grounding) downgraded() bool {
	return len(g.invalid) > 0 || g.unsupported != nil && len(g.seen) == 0
}

// warnings describe problems that remained after the retry in an answer
// that was kept.
func (g grounding) warnings() []string {
	if g.downgraded() {
		return nil
	}
	var out []string
	if g.unsupported != nil {
		out = append(out, fmt.Sprintf("the conclusion names %s, but the evidence shows the signature of %s instead", g.unsupported.category, categories(g.seen)))
	}
	if g.symptomIn != nil {
		out = append(out, fmt.Sprintf("the conclusion does not cite the evidence that shows the failure (%s)", strings.Join(g.symptomIn, ", ")))
	}
	if g.setting != "" {
		out = append(out, fmt.Sprintf("the conclusion does not cite the evidence that shows the setting %s (%s)", g.setting, strings.Join(g.settingIn, ", ")))
	}
	return out
}

// note tells the model what the checks found. It contains only evidence
// IDs, category names, Hody's own rule summaries and the setting's name,
// never evidence text, because it sits outside the data delimiters.
func (g grounding) note(s *model.Session) string {
	var b strings.Builder
	b.WriteString("NOTE: Hody checked your previous answer.\n")
	if len(g.invalid) > 0 {
		fmt.Fprintf(&b, "- It cited evidence IDs that do not exist: %s. Existing IDs are %s. Cite only existing IDs.\n",
			strings.Join(g.invalid, ", "), strings.Join(evidenceIDs(s), ", "))
	}
	if r := g.unsupported; r != nil && len(g.seen) == 0 {
		fmt.Fprintf(&b, "- It named the category %s, but nothing in the evidence shows that failure (Hody recognises it by: %s). The root cause must explain the failure the evidence does show. Name a cause the evidence shows, or answer insufficient_evidence; a different category name for the same unsupported cause is not an answer.\n",
			r.category, r.summary)
	} else if r != nil {
		fmt.Fprintf(&b, "- It named the category %s, but the evidence shows the signature of a different known failure:", r.category)
		for _, h := range g.seen {
			fmt.Fprintf(&b, " %s (%s, in %s);", h.Category, h.Summary, strings.Join(h.EvidenceIDs, ", "))
		}
		b.WriteString(" If your cause is that failure, use its category; if not, explain how your cause produces the failure the evidence shows.\n")
	}
	if g.symptomIn != nil {
		fmt.Fprintf(&b, "- The failure it names is shown in %s, which it did not cite.\n", strings.Join(g.symptomIn, ", "))
	}
	if g.setting != "" {
		fmt.Fprintf(&b, "- It names the setting %s, which appears in %s, which it did not cite.\n", g.setting, strings.Join(g.settingIn, ", "))
	}
	if len(g.invalid) == 0 && g.unsupported == nil {
		b.WriteString("If your conclusion is right, keep it and add the missing evidence IDs.\n")
	}
	return b.String()
}

func categories(hs []model.Hypothesis) string {
	var names []string
	for _, h := range hs {
		names = append(names, h.Category)
	}
	return strings.Join(names, ", ")
}

// ruleFor returns the triage rule of a known category, or nil.
func ruleFor(category string) *triageRule {
	for i := range triageRules {
		if triageRules[i].category == category {
			return &triageRules[i]
		}
	}
	return nil
}

// signatureEvidence returns the IDs of evidence matching a category's rule.
func signatureEvidence(evidence []model.Evidence, category string) []string {
	for _, h := range matchRules(evidence) {
		if h.Category == category {
			return h.EvidenceIDs
		}
	}
	return nil
}

var (
	settingNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{1,63}$`)
	bracketsRE    = regexp.MustCompile(`\[[^\]]*\]`)
)

// settingName is the last named part of a field path, ignoring anything
// in brackets, e.g. memory for
// spec.template.spec.containers[app].resources.limits.memory, or "" if
// there is none worth looking for.
func settingName(field string) string {
	parts := strings.FieldsFunc(bracketsRE.ReplaceAllString(field, " "), func(r rune) bool { return strings.ContainsRune("./ ,", r) })
	for i := len(parts) - 1; i >= 0; i-- {
		if settingNameRE.MatchString(parts[i]) {
			return parts[i]
		}
	}
	return ""
}

// evidenceNaming returns the IDs of evidence containing name as a word.
func evidenceNaming(evidence []model.Evidence, name string) []string {
	re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(name) + `($|[^A-Za-z0-9_])`)
	var ids []string
	for _, ev := range evidence {
		if re.MatchString(ev.Text) {
			ids = append(ids, ev.ID)
		}
	}
	return ids
}

func overlaps(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// splitCitations separates cited IDs into existing and non-existing ones.
func splitCitations(s *model.Session, ids []string) (valid, invalid []string) {
	for _, id := range ids {
		if s.FindEvidence(id) != nil {
			valid = append(valid, id)
		} else {
			invalid = append(invalid, id)
		}
	}
	return valid, invalid
}

func evidenceIDs(s *model.Session) []string {
	ids := make([]string, len(s.Evidence))
	for i, ev := range s.Evidence {
		ids[i] = ev.ID
	}
	return ids
}
