// Package eval loads scenario folders, runs them in replay mode (recorded
// tool outputs and a scripted model) or live mode (real Docker or cluster
// and a real model), grades the results and prints a table. Replay proves
// plumbing and security properties; it does not measure accuracy, because
// the model's answers are scripted.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"hody/internal/gateway"
	"hody/internal/llm"
	"hody/internal/model"
	"hody/internal/pipeline"
	"hody/internal/store"
	"hody/internal/tools"
)

// Label is the ground truth of a scenario (label.json).
type Label struct {
	Name           string       `json:"name"`
	Description    string       `json:"description"`
	Target         model.Target `json:"target"`
	Category       string       `json:"category"`
	FaultyObject   string       `json:"faulty_object"`
	FaultyField    string       `json:"faulty_field"`
	MustCite       []string     `json:"must_cite_tools"`
	Canaries       []string     `json:"canaries"`
	InjectionLine  string       `json:"injection_line"`
	ExpectOutcome  string       `json:"expect_outcome"`
	ExpectVerdict  string       `json:"expect_verdict,omitempty"`
	ExpectRejected int          `json:"expect_rejected_requests,omitempty"`
	LiveSelector   string       `json:"live_selector,omitempty"` // k8s live mode: finds the generated pod name
}

// Scenario is one loaded scenario folder.
type Scenario struct {
	Dir        string
	Label      Label
	Recordings []tools.Recording
	Script     []llm.Scripted
}

// Result is what one run produced, for grading.
type Result struct {
	Scenario   Scenario
	Session    *model.Session
	RunErr     error
	RequestDir string   // saved outgoing model requests
	Missing    []string // replay: purposes asked with no scripted answer
	Unused     []string // replay: scripted answers never asked for
}

// Load reads a scenario folder.
func Load(dir string) (Scenario, error) {
	sc := Scenario{Dir: dir}
	if err := readJSON(filepath.Join(dir, "label.json"), &sc.Label); err != nil {
		return sc, err
	}
	if err := readJSON(filepath.Join(dir, "transcript.json"), &sc.Script); err != nil {
		return sc, err
	}
	recs, err := tools.LoadRecordings(dir)
	sc.Recordings = recs
	return sc, err
}

// LoadAll loads every scenario folder under root, sorted by name.
func LoadAll(root string) ([]Scenario, error) {
	dirs, err := filepath.Glob(filepath.Join(root, "*", "label.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	var out []Scenario
	for _, d := range dirs {
		sc, err := Load(filepath.Dir(d))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Dir(d), err)
		}
		out = append(out, sc)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no scenarios (folders with label.json) under %s", root)
	}
	return out, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// RunReplay runs a scenario against its recordings and scripted model,
// in test mode: canaries from the label arm the tripwire and every
// outgoing model request is saved under work/<name>/requests.
func RunReplay(ctx context.Context, sc Scenario, work string) Result {
	res := Result{Scenario: sc, RequestDir: filepath.Join(work, sc.Label.Name, "requests")}
	srv := llm.NewReplay(sc.Script)
	defer srv.Close()
	gw := gateway.New()
	reg := tools.NewRegistry(gw, sc.Label.Target, tools.Replayed(tools.Specs(sc.Label.Target.Platform), sc.Dir, sc.Recordings)...)
	client, err := llm.New(llm.Config{BaseURL: srv.URL, Model: "replay", Canaries: sc.Label.Canaries, RecordDir: res.RequestDir})
	if err != nil {
		res.RunErr = err
		return res
	}
	res.Session, res.RunErr = run(ctx, sc, work, pipeline.Deps{Tools: reg, LLM: client, Gateway: gw})
	srv.Close()
	res.Missing, res.Unused = srv.Missing, srv.Unused()
	return res
}

func run(ctx context.Context, sc Scenario, work string, d pipeline.Deps) (*model.Session, error) {
	st, err := store.Open(filepath.Join(work, sc.Label.Name))
	if err != nil {
		return nil, err
	}
	d.Store = st
	s := pipeline.NewSession(sc.Label.Target)
	return s, pipeline.New(d).Run(ctx, s)
}

// ToolFactory builds real tools for a target (Docker client or cluster).
type ToolFactory func(t model.Target) ([]tools.Tool, error)

// PodFinder returns the name of a pod matching a label selector, for live
// Kubernetes scenarios whose pod names are generated.
type PodFinder func(ctx context.Context, namespace, selector string) (string, error)

// RunLive runs a scenario against a real system and a real model. The
// scenario's live/setup.sh must have been run by a person first; Hody
// itself never creates or changes anything. Canaries from the label arm
// the tripwire, and requests are saved for grading.
func RunLive(ctx context.Context, sc Scenario, work string, cfg llm.Config, factory ToolFactory, find PodFinder) Result {
	res := Result{Scenario: sc, RequestDir: filepath.Join(work, sc.Label.Name, "requests")}
	if sc.Label.LiveSelector != "" && sc.Label.Target.Platform == model.PlatformK8s {
		pod, err := find(ctx, sc.Label.Target.Namespace, sc.Label.LiveSelector)
		if err != nil {
			res.RunErr = fmt.Errorf("finding the live pod (did you run live/setup.sh?): %w", err)
			return res
		}
		sc.Label.Target.Pod = pod
		res.Scenario = sc
	}
	cfg.Canaries, cfg.RecordDir = sc.Label.Canaries, res.RequestDir
	client, err := llm.New(cfg)
	if err != nil {
		res.RunErr = err
		return res
	}
	list, err := factory(sc.Label.Target)
	if err != nil {
		res.RunErr = err
		return res
	}
	gw := gateway.New()
	reg := tools.NewRegistry(gw, sc.Label.Target, list...)
	res.Session, res.RunErr = run(ctx, sc, work, pipeline.Deps{Tools: reg, LLM: client, Gateway: gw})
	return res
}
