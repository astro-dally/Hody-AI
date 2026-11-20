# Architecture

## Overview

```
 you ──► cmd/hody (CLI)  or  internal/web (127.0.0.1 page)
                │
                ▼
        internal/pipeline ── fixed steps ──► internal/store (session JSON, audit.jsonl)
           │          │
           │          └──► internal/llm  ── tripwire ──► model (OpenAI-compatible API)
           ▼
        internal/tools (registry: validate + scope lock)
           │                     ▲
           │ raw text            │ evidence only
           ▼                     │
   tool Run functions ──► internal/gateway (redact → scan → limit → number → wrap)
   (bound by internal/tools/docker and internal/tools/k8s)
```

## The pipeline

`internal/pipeline/run.go` holds the step list. Every step has the same
signature, `func(ctx context.Context, s *model.Session) error`, and `Run`
calls them in this order, stopping at the first error:

```
 1 collect      fixed first-look tools (tools.FirstLook)
 2 triage       code rules; model only if no rule matches
 3 investigate  model may request up to 8 more tools; each validated first
 4 conclude     root cause with evidence IDs, or insufficient_evidence;
                IDs checked in code, one retry, then downgrade
 5 propose_fix  unified diff against the manifest evidence (skipped on abstention)
 6 review_fix   diff applied in memory, policy rules on before/after,
                critic call with cause + diff only; stricter verdict wins
 7 approve      session complete; shown to the user, nothing applied
```

First-look tools:

| Docker | Kubernetes |
|---|---|
| `docker.inspect_container` | `k8s.pod_status` |
| `docker.logs` | `k8s.events` (this pod) |
| `docker.events` | `k8s.logs` (current) |
| `docker.config` (compose-style file) | `k8s.logs` (previous container) |
| | `k8s.what_changed` (ReplicaSet template diff) |
| | `k8s.manifest` (owning Deployment as YAML) |

If you pass `--file`, `local.file` is also collected and becomes the file
the fix targets. Evidence IDs follow collection order (`E1`, `E2`, ...).

Model calls have five purposes (`triage`, `investigate`, `conclude`, `fix`,
`critic`). Each answer is parsed into a Go struct with unknown and missing
fields rejected (`internal/llm/parse.go`), with one retry.

## The gateway

`gateway.Process(tool, args, raw)` is the only path from a tool's raw
output to anything else. The registry calls it; nothing else sees `raw`.

```
 raw text ──► redact (full text) ──► sanitize ──► scan ──► limit ──► number ──► wrap
              key names                ANSI,     9 heuristics  head 60      E1, E2…     <<<HODY_DATA id=E3
              known formats            control   flag, keep    tail 80      tool, args,  tool=… nonce=…>>>
              entropy                  chars                   error and    time,        numbered lines,
              [SECRET_n] placeholders                          flagged      SHA-256      flagged lines quoted
                                                               lines        of raw       <<<HODY_DATA_END …>>>
```

- The placeholder map (`secret value → [SECRET_n]`) is an unexported field
  of the per-session `Gateway`. Once a value is known it is replaced in
  every later tool result of the session.
- Redaction runs before limiting, so truncation cannot split a secret.
- The delimiter nonce is random per session; `<<<` and `>>>` inside data
  are broken up so data cannot close the block.
- `Evidence.Text` is the redacted, limited text (shown to you and used for
  diffs); `Evidence.Block` is the wrapped form put into prompts.

## The layering rule

`internal/pipeline` must not reach `internal/tools/docker` or
`internal/tools/k8s`. It goes through `internal/tools`, which only returns
gateway evidence. To make this hold for the whole import graph, not just
direct imports, tool *specs* (names, argument schemas) live in
`internal/tools`, and the *binding* of specs to clients lives in the client
packages (`docker.Tools`, `k8s.Tools`), which `cmd/hody` and `eval`
assemble. `TestLayering` (`internal/pipeline/layering_test.go`) walks the
transitive import graph from the pipeline and fails if it reaches either
client package or `k8s.io/...`.

## Tool validation and scope

`tools.Registry` is created with the session target and keeps it for its
life. Before a request runs: the tool must be registered, it must be for
the session's platform, every required argument must be present, no unknown
argument may be present, every value must match its pattern, and any
argument named `namespace`, `container` or `path` must equal the target's
value. A rejected request runs nothing, is recorded with its reason, counts
against the 8-request budget and is shown to the model in the next prompt.

## Data types (`internal/model`)

```
Session
├─ ID, Target{Platform, Container | Namespace+Pod, File}, Created, Status, Step, Error
├─ Evidence[]   {ID, Tool, Args, Time, RawSHA256, Text, Block, Flags[], Redactions, Omitted}
├─ Hypotheses[] {Source ("rule:…" or "model"), Category, Summary, EvidenceIDs}
├─ ToolCalls[]  {Tool, Args, By, Accepted, Reason, EvidenceID}
├─ RootCause    {Outcome, Category, Summary, Object, Field, EvidenceIDs, NextChecks, Note}
├─ Fix          {EvidenceID, Diff, Explanation, ApplyError}
├─ Review       {Verdict, PolicyVerdict, CriticVerdict, CriticReason, Findings[]}
└─ Warnings[]
```

Everything in a `Session` is redacted text or model output, which is why
it can be stored as JSON.

## Packages and files

| Path | Purpose |
|---|---|
| `cmd/hody/main.go` | command dispatch and usage text |
| `cmd/hody/config.go` | environment configuration, interleaved flag parsing |
| `cmd/hody/diagnose.go` | `hody diagnose`; wires real tools and the model |
| `cmd/hody/k8s.go` | builds the Kubernetes tools from a kubeconfig |
| `cmd/hody/report.go` | terminal report |
| `cmd/hody/serve.go` | `hody serve`; 127.0.0.1 only |
| `cmd/hody/eval.go` | `hody eval`; table and `results.json` |
| `internal/model/model.go` | shared data types |
| `internal/gateway/gateway.go` | `Process`, numbering, wrapping, sanitising |
| `internal/gateway/redact.go` | three redaction layers, placeholders |
| `internal/gateway/scan.go` | injection heuristics |
| `internal/gateway/limit.go` | size limiting |
| `internal/llm/client.go` | chat client, tripwire, request recording, `Ask` with retry |
| `internal/llm/parse.go` | strict typed parsing |
| `internal/llm/replay.go` | fake model server replaying scripted answers |
| `internal/tools/registry.go` | registry, validation, scope lock |
| `internal/tools/docker_tools.go` | Docker tool specs, `local.file`, `WithRuns` |
| `internal/tools/k8s_specs.go` | Kubernetes tool specs |
| `internal/tools/firstlook.go` | first-look sets, manifest tools, specs per platform |
| `internal/tools/replay.go` | tools bound to recorded outputs |
| `internal/tools/docker/client.go` | GET-only, allowlisted Unix-socket client; log demux |
| `internal/tools/docker/render.go` | API types, text rendering, compose-style file |
| `internal/tools/docker/tools.go` | binds Docker specs to the client |
| `internal/tools/docker/dockertest/fake.go` | fake Docker API on `httptest` |
| `internal/tools/k8s/client.go` | kubeconfig, pod status, events, logs |
| `internal/tools/k8s/objects.go` | Deployments, ReplicaSets, what changed, ConfigMap keys, manifest |
| `internal/tools/k8s/cluster.go` | Services and EndpointSlices, nodes, quotas, line diff |
| `internal/tools/k8s/render.go` | rendering helpers |
| `internal/tools/k8s/tools.go` | binds Kubernetes specs to the client |
| `internal/pipeline/run.go` | step order, `Run`, audit, tool and model call helpers |
| `internal/pipeline/collect.go` … `approve.go` | one file per step |
| `internal/pipeline/prompts.go` | system prompt, task texts, answer types |
| `internal/policy/rules.go` | eleven fix policy rules, verdicts |
| `internal/policy/patch.go` | in-memory unified diff application |
| `internal/store/store.go` | session files and audit log |
| `internal/web/server.go` | handlers, host/origin guard, CSRF |
| `internal/web/templates/*.html`, `static/style.css` | embedded page |
| `eval/scenario.go` | scenario loading, replay and live runs |
| `eval/grade.go` | grader and results table |
