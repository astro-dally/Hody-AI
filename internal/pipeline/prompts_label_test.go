package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The model is told Hody's known categories; every labelled category must
// be one of them (or "unknown" for the abstention scenario).
func TestPromptsListKnownCategories(t *testing.T) {
	for _, c := range KnownCategories() {
		if !strings.Contains(concludeTask, c) || !strings.Contains(triageTask, c) {
			t.Errorf("category %s missing from the prompts", c)
		}
	}
	for _, l := range labels(t) {
		if l.Category != "unknown" && !slices.Contains(KnownCategories(), l.Category) {
			t.Errorf("%s: label category %q is not a known category", l.Name, l.Category)
		}
	}
}

// The prompt examples must not hand the model a scenario's answer: no
// labelled object name or faulty field appears in the task text.
func TestPromptsDoNotLeakScenarioAnswers(t *testing.T) {
	prompts := strings.ToLower(triageTask + "\n" + concludeTask)
	word := func(s string) *regexp.Regexp {
		return regexp.MustCompile(`(^|[^a-z0-9_-])` + regexp.QuoteMeta(strings.ToLower(s)) + `($|[^a-z0-9_-])`)
	}
	for _, l := range labels(t) {
		var answers []string
		if l.FaultyObject != "" {
			answers = append(answers, l.FaultyObject, l.FaultyObject[strings.LastIndexAny(l.FaultyObject, "/ ")+1:])
		}
		if l.FaultyField != "" {
			parts := strings.FieldsFunc(l.FaultyField, func(r rune) bool { return r == '.' || r == '[' || r == ']' })
			answers = append(answers, l.FaultyField, parts[len(parts)-1])
		}
		for _, a := range answers {
			if word(a).MatchString(prompts) {
				t.Errorf("%s: the prompts contain %q, part of the labelled answer", l.Name, a)
			}
		}
	}
}

type scenarioLabel struct {
	Name         string `json:"name"`
	Category     string `json:"category"`
	FaultyObject string `json:"faulty_object"`
	FaultyField  string `json:"faulty_field"`
}

func labels(t *testing.T) []scenarioLabel {
	t.Helper()
	files, _ := filepath.Glob("../../eval/scenarios/*/label.json")
	if len(files) < 12 {
		t.Fatalf("found %d labels", len(files))
	}
	var out []scenarioLabel
	for _, f := range files {
		var l scenarioLabel
		b, err := os.ReadFile(f)
		if err != nil || json.Unmarshal(b, &l) != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out = append(out, l)
	}
	return out
}
