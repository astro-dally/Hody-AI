package pipeline

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"hody/internal/gateway"
	"hody/internal/llm"
	"hody/internal/model"
	"hody/internal/tools"
)

var target = model.Target{Platform: model.PlatformDocker, Container: "web"}

const manifestText = "services:\n  web:\n    image: shop/web:1.0\n    mem_limit: 67108864\n"

// harness builds an engine over fake Docker tools and a replay model.
func harness(t *testing.T, logs string, script ...llm.Scripted) (*Engine, *llm.Replay) {
	t.Helper()
	run := func(text string) tools.RunFunc {
		return func(context.Context, map[string]string) (string, error) { return text, nil }
	}
	list := tools.WithRuns(tools.DockerSpecs(), map[string]tools.RunFunc{
		"docker.list_containers": run("web exited"), "docker.inspect_container": run("state: status=exited exitCode=1"),
		"docker.logs": run(logs), "docker.events": run("no events"), "docker.inspect_image": run("image"),
		"docker.config": run(manifestText),
	})
	srv := llm.NewReplay(script)
	t.Cleanup(srv.Close)
	client, err := llm.New(llm.Config{BaseURL: srv.URL, Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	gw := gateway.New()
	return New(Deps{Tools: tools.NewRegistry(gw, target, list...), LLM: client, Gateway: gw}), srv
}

func answer(purpose string, v any) llm.Scripted {
	b, _ := json.Marshal(v)
	return llm.Scripted{Purpose: purpose, Answer: b}
}

func done() llm.Scripted {
	return answer("investigate", map[string]any{"action": "done", "tool": "", "args": map[string]string{}, "reason": "enough"})
}

func cause(ids ...string) llm.Scripted {
	return answer("conclude", map[string]any{"outcome": "root_cause", "category": "oom_killed", "summary": "out of memory",
		"object": "container web", "field": "mem_limit", "evidence_ids": ids, "next_checks": []string{}})
}

func fix() llm.Scripted {
	return answer("fix", map[string]any{"diff": "--- a/x\n+++ b/x\n@@ -4,1 +4,1 @@\n-    mem_limit: 67108864\n+    mem_limit: 268435456\n", "explanation": "more memory"})
}

func critic(v string) llm.Scripted {
	return answer("critic", map[string]any{"verdict": v, "reason": "ok"})
}

func TestStepOrderIsFixed(t *testing.T) {
	e, _ := harness(t, "")
	want := []string{"collect", "triage", "investigate", "conclude", "propose_fix", "review_fix", "approve"}
	if got := e.Steps(); !slices.Equal(got, want) {
		t.Fatalf("steps %v", got)
	}
}

func TestTriageRules(t *testing.T) {
	cases := []struct {
		line, want string
	}{
		{"state: status=exited exitCode=137 oomKilled=true", "oom_killed"},
		{"lastState: terminated reason=OOMKilled exitCode=137", "oom_killed"},
		{"2026-11-17T09:14:09Z container oom name=web", "oom_killed"},
		{"state: status=exited exitCode=126", "command_not_executable"},
		{"exec /usr/local/bin/start.sh: permission denied", "command_not_executable"},
		{"state: status=exited exitCode=127", "command_not_found"},
		{`exec: "gunicron": executable file not found in $PATH`, "command_not_found"},
		{"waiting reason=ImagePullBackOff", "image_pull"},
		{"Failed to pull image \"shop/api:1.9\": manifest unknown", "image_pull"},
		{"Warning Unhealthy Liveness probe failed: HTTP probe failed with statuscode: 404", "probe_failure"},
		{"Readiness probe failed: connection refused", "probe_failure"},
		{"waiting reason=CreateContainerConfigError message=couldn't find key database_url in ConfigMap shop/app-config", "config_reference"},
		{"Warning FailedScheduling 0/3 nodes are available: 3 Insufficient memory.", "unschedulable"},
		{"listen tcp 0.0.0.0:3000: bind: address already in use", "port_conflict"},
		{"Bind for 0.0.0.0:8080 failed: port is already allocated", "port_conflict"},
		{"Error: EACCES: permission denied, open '/data/tmp.bin'", "mount_permission"},
		{"open /var/lib/app/db.sqlite: permission denied", "mount_permission"},
		{"FATAL: environment variable DB_HOST is not set", "missing_env"},
		{"KeyError: 'DATABASE_URL'", "missing_env"},
		{"state: status=exited exitCode=1 oomKilled=false", ""},
		{"exitCode=1370", ""},
		{"INFO all probes configured", ""},
		{"user admin logged in", ""},
	}
	for _, c := range cases {
		hs := matchRules([]model.Evidence{{ID: "E1", Text: c.line}})
		var got []string
		for _, h := range hs {
			got = append(got, h.Category)
		}
		if c.want == "" && len(got) != 0 || c.want != "" && !slices.Contains(got, c.want) {
			t.Errorf("%q: got %v want %q", c.line, got, c.want)
		}
	}
}

func TestFullRunWithRuleMatchSkipsModelTriage(t *testing.T) {
	e, srv := harness(t, "Killed\nexitCode=137", done(), cause("E1", "E2"), fix(), critic("approve"))
	s := NewSession(target)
	if err := e.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if s.Status != model.StatusComplete || s.RootCause.Outcome != model.OutcomeRootCause || s.Fix == nil {
		t.Fatalf("session %+v", s)
	}
	if len(srv.Missing) != 0 || len(srv.Unused()) != 0 {
		t.Fatalf("missing %v unused %v", srv.Missing, srv.Unused())
	}
	if s.Hypotheses[0].Source != "rule:oom-killed" {
		t.Fatalf("hypotheses %+v", s.Hypotheses)
	}
}

func TestTriageAsksModelWhenNoRuleMatches(t *testing.T) {
	tri := answer("triage", map[string]any{"hypotheses": []map[string]any{{"category": "unknown", "summary": "s", "evidence_ids": []string{"E2", "E99"}}}})
	abstain := answer("conclude", map[string]any{"outcome": "insufficient_evidence", "category": "", "summary": "", "object": "",
		"field": "", "evidence_ids": []string{}, "next_checks": []string{"enable debug logs"}})
	e, srv := harness(t, "exited with status 1", tri, done(), abstain)
	s := NewSession(target)
	if err := e.Run(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if len(s.Hypotheses) != 1 || !slices.Equal(s.Hypotheses[0].EvidenceIDs, []string{"E2"}) {
		t.Fatalf("hypotheses %+v (invalid IDs must be dropped)", s.Hypotheses)
	}
	if s.RootCause.Outcome != model.OutcomeInsufficient || s.Fix != nil {
		t.Fatal("abstention must not produce a fix")
	}
	if len(srv.Unused()) != 0 {
		t.Fatalf("unused %v", srv.Unused())
	}
}

func TestConcludeCitationCheck(t *testing.T) {
	cases := []struct {
		name     string
		answers  []llm.Scripted
		outcome  string
		downgrad bool
	}{
		{"valid", []llm.Scripted{cause("E1")}, model.OutcomeRootCause, false},
		{"retry fixes it", []llm.Scripted{cause("E1", "E42"), cause("E2")}, model.OutcomeRootCause, false},
		{"downgraded", []llm.Scripted{cause("E42"), cause("E1", "E43")}, model.OutcomeInsufficient, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, srv := harness(t, "exitCode=137", c.answers...)
			s := NewSession(target)
			if err := e.collect(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if err := e.conclude(context.Background(), s); err != nil {
				t.Fatal(err)
			}
			if s.RootCause.Outcome != c.outcome || (s.RootCause.Note != "") != c.downgrad {
				t.Fatalf("got %+v", s.RootCause)
			}
			for _, id := range s.RootCause.EvidenceIDs {
				if s.FindEvidence(id) == nil {
					t.Fatalf("kept invalid citation %s", id)
				}
			}
			if len(srv.Unused()) != 0 {
				t.Fatalf("unused answers %v", srv.Unused())
			}
		})
	}
}

func TestInvestigateBudgetCountsRejections(t *testing.T) {
	var script []llm.Scripted
	for i := 0; i < 10; i++ {
		args := map[string]string{"container": "web"}
		if i%2 == 0 {
			args = map[string]string{"container": "other"} // out of scope
		}
		script = append(script, answer("investigate", map[string]any{"action": "request_tool", "tool": "docker.events", "args": args, "reason": "r"}))
	}
	e, srv := harness(t, "", script...)
	s := NewSession(target)
	if err := e.investigate(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if s.ModelRequests() != MaxModelRequests || len(srv.Bodies) != MaxModelRequests {
		t.Fatalf("requests %d, model calls %d", s.ModelRequests(), len(srv.Bodies))
	}
	if len(s.Warnings) != 1 {
		t.Fatal("expected a limit warning")
	}
	if !strings.Contains(string(srv.Bodies[1]), "REJECTED") || !strings.Contains(string(srv.Bodies[1]), "outside this session") {
		t.Fatal("rejection reason was not returned to the model")
	}
}
