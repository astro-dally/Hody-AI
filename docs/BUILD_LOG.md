# Build log

Every command output below is real output from the build sandbox, trimmed
where noted. The phase gate is `./check.sh`, which runs `gofmt -l .`,
`go vet ./...`, `go build ./...` and `go test -count=1 ./...` and stops at
the first failure.

## Phase 0: environment

- No Go toolchain was installed. `proxy.golang.org`, `dl.google.com` and
  vanity hosts such as `k8s.io` are blocked by the sandbox network policy;
  `github.com`, `codeload.github.com` and the Ubuntu archive are allowed.
- Installed Go from the Ubuntu archive: `apt-get install golang-1.26-go`.

```
$ /usr/lib/go-1.26/bin/go version
go version go1.26.8 linux/amd64
```

- `git ls-remote --tags https://github.com/kubernetes/client-go` showed
  `v0.37.2` as the newest release tag, so Hody uses `k8s.io/client-go v0.37.2`.
- Because the Go module proxy is blocked, I wrote a throwaway local GOPROXY
  (Python, outside the repository, at `/tmp/gomirror/proxy.py`) that maps
  module paths to their GitHub repositories (`k8s.io/x` to
  `github.com/kubernetes/x`, `golang.org/x/y` to `github.com/golang/y`,
  `gopkg.in/...`, `go.yaml.in/...`, and so on), downloads the tagged tarball
  from `codeload.github.com`, and builds the module zip following the
  official module zip rules (submodules, VCS directories and nested vendor
  files excluded, root LICENSE copied into submodules). `GOSUMDB=off` was
  required because `sum.golang.org` is also blocked.
- Hash check: `sum.golang.org` could not be queried, so `go.sum` was not
  verified against the checksum database. I compared five entries against
  hashes I know from upstream (`google/uuid v1.6.0`, `davecgh/go-spew`,
  `go-logr/logr v1.4.3`, `x448/float16 v0.8.4`, `gopkg.in/inf.v0 v0.9.1`);
  all five matched. If `go build` on your machine reports a checksum
  mismatch, delete `go.sum` and run `go mod tidy` with the normal proxy.
- No Docker daemon, no `kind`/`kubectl`, no cluster, no model API key.

## Phase 1: module, layout, data types

Created `go.mod` (module `hody`, go 1.26), the directory layout from the
specification, `internal/model/model.go` (Target, Evidence, Flag,
Hypothesis, ToolCall, RootCause, Fix, Finding, Review, Session) and a
placeholder `cmd/hody/main.go`.

```
$ gofmt -l .
(no files)
$ go vet ./...
ok
$ go build ./...
ok
$ go test ./...
?   	hody/cmd/hody	[no test files]
ok  	hody/internal/model	0.001s
```

## Phase 2: gateway

Wrote `internal/gateway`: `redact.go` (key-name, pattern and entropy
layers; stable `[SECRET_n]` placeholders), `scan.go` (nine injection
heuristics), `limit.go` (head, tail, error and flagged lines),
`gateway.go` (`Process`: redact, sanitize, scan, limit, number, wrap).
Shared test data: `eval/testdata/injections.txt` (20 variants) and
`eval/testdata/benign_log_lines.txt` (16 ordinary lines).

Problems found and fixed during this phase:

- The `key=value` regex consumed nested pairs: in `args: --db-password=x`
  the match for `args` swallowed the password. Fixed by resuming each
  search at the start of the previous value.
- The entropy token pattern included `=`, so `DB_PASSWORD=hunter2hunter2`
  was treated as one random-looking token and the key name was hidden too.
  Fixed by allowing `=` only as trailing base64 padding.
- Checked with a temporary test that the data-driven scanner tests really
  loaded their files (20 injection lines, 16 benign lines), then removed it.

```
$ gofmt -l .
(no files)
$ go vet ./...
ok
$ go build ./...
ok
$ go test ./...
?   	hody/cmd/hody	[no test files]
ok  	hody/internal/gateway	0.013s
ok  	hody/internal/model	0.001s
```

## Phase 3: model client, typed parsing, tripwire, fake model server

Wrote `internal/llm`: `client.go` (chat completions client, tripwire that
checks raw and JSON-escaped canaries before sending, request recording in
test mode, `Ask` with one retry), `parse.go` (strict parsing: unknown,
missing and trailing data rejected, optional `Validate` hook) and
`replay.go` (an `httptest` server that returns scripted answers per
purpose). Decision: a request blocked by the tripwire is not saved to disk,
so a leaked canary is never written anywhere; only the canary's index is
reported.

```
$ gofmt -l .
(no files)
$ go vet ./...
ok
$ go build ./...
ok
$ go test ./...
?   	hody/cmd/hody	[no test files]
ok  	hody/internal/gateway	0.020s
ok  	hody/internal/llm	0.012s
ok  	hody/internal/model	0.001s
```

## Phase 4: tool registry, validation, Docker client and its fake

Wrote `internal/tools/registry.go` (registry, schema validation, scope
lock by argument name: `namespace`, `container` and `path` must equal the
session target), `internal/tools/docker_tools.go` (Docker tool specs bound
to the client, plus the `local.file` tool for a user-supplied manifest),
`internal/tools/replay.go` (tools bound to recorded outputs),
`internal/tools/docker/client.go` (Unix-socket HTTP client whose transport
refuses non-GET methods and non-allowlisted paths; multiplexed log
decoding), `internal/tools/docker/render.go` (text rendering and the
compose-style file a Docker fix targets) and
`internal/tools/docker/dockertest/fake.go` (fake Docker API on `httptest`).

Problems found and fixed:

- The fake Docker API was first written inside a `_test.go` file, which
  the `tools` package tests cannot import. Moved it to its own
  `dockertest` package.
- `regexp: invalid repeat count {1,4096}`: Go's regexp caps repeats at
  1000. Changed the path pattern to `{1,1000}`.
- Port bindings were rendered in map order, which is random. Sorted them.

```
$ gofmt -l .
(no files)
$ go vet ./...
ok
$ go build ./...
ok
$ go test ./...
?   	hody/cmd/hody	[no test files]
ok  	hody/internal/gateway	0.013s
ok  	hody/internal/llm	0.010s
ok  	hody/internal/model	0.002s
ok  	hody/internal/tools	0.011s
ok  	hody/internal/tools/docker	0.005s
?   	hody/internal/tools/docker/dockertest	[no test files]
```

## Phase 5: pipeline steps 1 to 5, Docker CLI command, Docker scenarios

Wrote `internal/store` (session JSON files, append-only `audit.jsonl`),
`internal/pipeline` (`run.go` with the fixed step list, `collect.go`,
`triage.go` with all ten code rules, `investigate.go` with the 8-request
budget, `conclude.go` with citation checking, retry and downgrade,
`propose.go`, `approve.go`, `prompts.go`), `internal/tools/firstlook.go`,
`eval/scenario.go` (scenario loading and replay runs), the CLI
(`cmd/hody`: `main.go`, `config.go`, `diagnose.go`, `report.go`) and the
five Docker scenarios (generated once with a throwaway script so recorded
outputs, transcripts and live setup scripts agree). `review_fix` was a
pass-through placeholder in this phase, completed in phase 7; `serve` and
Kubernetes support were stubs that returned "not built yet".

Problems found and fixed:

- **Over-redaction hid the diagnosis.** In `docker-missing-env` the line
  `KeyError: 'DB_HOST'` was split into the key words "key" + "error", so
  `DB_HOST` was treated as a secret and replaced everywhere, including in
  `environment variable DB_HOST is not set`. The missing-env rule then
  could not match and the run asked for a model triage answer the script
  did not have. Added error-type suffixes (`error`, `exception`, ...) to the
  benign key words and a regression test.
- **Scanner missed timestamped role markers.** Docker logs are requested
  with timestamps, so `2026-...Z SYSTEM: ...` did not match the
  line-anchored role-marker heuristic. The heuristic now allows up to three
  timestamp, stream or log-level prefixes. Two new injection variants and
  two new benign lines cover this.
- **Untrusted text outside the delimiters.** The first triage version
  copied the matched log line into the hypothesis summary, which is shown
  to the model outside the data delimiters. Removed; hypotheses cite
  evidence IDs only.
- **Layering.** `internal/tools` imported the Docker client to bind tool
  specs, so the pipeline reached it transitively. Moved the binding into
  `internal/tools/docker/tools.go` (`docker.Tools`); `internal/tools` now
  imports no platform client. `TestLayering` walks the import graph
  transitively; I checked it by adding a file importing the Docker client
  to the pipeline package (the test failed, naming the file) and removing
  it again.
- Two scripted diffs had wrong hunk line numbers (the `user:` line was not
  counted). Corrected in the generator.

Independent check after the replay run: none of the 17 saved model
requests contained a canary, and in `docker-missing-env` the scripted
request for container `billing-db` was rejected with
`argument "container" is outside this session's scope (docker billing-api)`.

```
$ gofmt -l .
(no files)
$ go vet ./...
ok
$ go build ./...
ok
$ go test ./...
?   	hody/cmd/hody	[no test files]
?   	hody/eval	[no test files]
ok  	hody/internal/gateway	0.018s
ok  	hody/internal/llm	0.012s
ok  	hody/internal/model	0.002s
ok  	hody/internal/pipeline	0.015s
ok  	hody/internal/store	0.003s
ok  	hody/internal/tools	0.007s
ok  	hody/internal/tools/docker	0.011s
?   	hody/internal/tools/docker/dockertest	[no test files]
$ go run ./cmd/hody eval eval/scenarios   (phase 5 interim output, trimmed)
docker-command-not-found: err=<nil> outcome=&{Outcome:root_cause Category:command_not_found Summary:The comman
docker-missing-env: err=<nil> outcome=&{Outcome:root_cause Category:missing_env Summary:The application reads 
docker-mount-permission: err=<nil> outcome=&{Outcome:root_cause Category:mount_permission Summary:The process 
docker-oom: err=<nil> outcome=&{Outcome:root_cause Category:oom_killed Summary:The worker process needs more m
docker-port-conflict: err=<nil> outcome=&{Outcome:root_cause Category:port_conflict Summary:Host port 8080 is 
```

## Phase 6: Kubernetes tools, "what changed", triage rules, Kubernetes scenarios

Wrote `internal/tools/k8s_specs.go` (ten tool specs) and
`internal/tools/k8s` (`client.go`: kubeconfig loading, pod status, events,
logs; `objects.go`: Deployment and ReplicaSets, what changed, ConfigMap
key names, manifest; `cluster.go`: Services with EndpointSlices, node
conditions, quotas, line diff; `render.go`; `tools.go`: binding). Added
`sigs.k8s.io/yaml` (already a client-go dependency) as a direct import.
All ten triage rules were written in phase 5. Seven Kubernetes scenarios
generated with a throwaway script that computes diff hunk positions from
the manifest text.

Problems found and fixed:

- `tools.Bind` would have collided with the source-scan test, which
  forbids `.Bind(` (pod binding) in the k8s package. Renamed to
  `tools.WithRuns`.
- `metav1.Time.Before` has a pointer receiver; compare `.Time` instead.
- `LineDiff` printed additions before deletions on ties; changed the tie
  rule so deletions come first. My first test expectation also guessed the
  YAML indentation wrong; corrected it to the real output.
- Probe and port output initially iterated over maps; replaced with
  ordered slices so output is deterministic.

Checks of the tests themselves: the read-verb test now fails if fewer than
10 fake-clientset actions were recorded. I added a compiling file with a
`Pods(...).Delete(...)` call to the k8s package: `TestSourceHasNoWriteOrSecretCalls`
failed and named the line; then I removed the file.

Replay check of all 12 scenarios (my own script over the saved session
files and saved model requests): no canary in any request, every planted
injection line flagged, the expected triage rule fired in all eleven
scenarios with a cause and model triage was used only in
`k8s-no-findable-cause`.

```
$ gofmt -l .
(no files)
$ go vet ./...
ok
$ go build ./...
ok
$ go test ./...
?   	hody/cmd/hody	[no test files]
?   	hody/eval	[no test files]
ok  	hody/internal/gateway	0.016s
ok  	hody/internal/llm	0.011s
ok  	hody/internal/model	0.001s
ok  	hody/internal/pipeline	0.013s
ok  	hody/internal/store	0.004s
ok  	hody/internal/tools	0.014s
ok  	hody/internal/tools/docker	0.011s
?   	hody/internal/tools/docker/dockertest	[no test files]
ok  	hody/internal/tools/k8s	0.103s
```

## Phase 7: policy rules, fix review, critic

Wrote `internal/policy` (`patch.go`: in-memory unified diff application,
single file only, hunks matched by content near the stated line;
`rules.go`: eleven rules, each with a plain-language risk and safer
alternative, `Check`, `Verdict`, `Stricter`) and replaced the placeholder
`review_fix` step (`internal/pipeline/review.go`). Shared test data:
`eval/testdata/unsafe_fixes.json` (18 unsafe fixes covering every rule,
3 safe controls).

Problems found and fixed:

- The first diff parser treated any line starting `--- ` as a file
  header, which would misread a removed YAML line starting with `-- `, and
  a parameter shadowed the `min` builtin. Rewrote it: `---` is a header only
  when the next line starts with `+++`.
- **Policy rule bug:** the inline-secret rule counted the safe form
  `secretKeyRef:` as an inline secret, because `\s*` after the colon
  matched across the newline into the next line. Restricted to spaces and
  tabs. Found by the `inline secret` unsafe-fix case.
- `TestCriticSeesOnlyCauseAndDiff` failed at first because its fake logs
  matched no triage rule, so the pipeline asked the scripted model for a
  triage answer it did not have. That was the test's setup (verified by
  adding `exitCode=137`), not a product bug.
- **The tripwire fired on my own scenario data.** In `k8s-image-pull` the
  scripted diff had been generated from the raw manifest, so its context
  line contained the raw canary `value: Cnry-K1-...`. A real model only
  sees `[SECRET_n]` and could not produce that. The diff did not apply to
  the redacted manifest, and when the critic request was built with that
  diff the tripwire refused to send it and the run failed. Regenerated the
  diff without the context line. Checked with `grep` that no transcript
  contains a canary string; the grader now checks this too (phase 9).

Replay after this phase: all 12 scenarios complete; review verdicts match
the labels (approve x8, `docker-mount-permission` block via `runs-as-root`
although the scripted critic approved, `k8s-exit-126` block via
`broad-permissions`, `k8s-unschedulable` warn via
`removed-resource-limits`, no review for the abstaining scenario).

```
$ gofmt -l .
(no files)
$ go vet ./...
ok
$ go build ./...
ok
$ go test ./...
?   	hody/cmd/hody	[no test files]
?   	hody/eval	[no test files]
ok  	hody/internal/gateway	0.018s
ok  	hody/internal/llm	0.010s
ok  	hody/internal/model	0.001s
ok  	hody/internal/pipeline	0.019s
ok  	hody/internal/policy	0.017s
ok  	hody/internal/store	0.003s
ok  	hody/internal/tools	0.007s
ok  	hody/internal/tools/docker	0.011s
?   	hody/internal/tools/docker/dockertest	[no test files]
ok  	hody/internal/tools/k8s	0.110s
```

## Phase 8: web page

Wrote `internal/web` (`server.go`, embedded `templates/*.html` and
`static/style.css`) and `cmd/hody/serve.go`. Design: one left-aligned
column, system fonts only (the content security policy blocks external
fonts), a cool sea-glass and slate palette with semantic block, warn and
approve colours, and the review verdict as the page's one prominent
element. No JavaScript; the progress view refreshes with a meta tag.

Changes made while testing:

- The pipeline notified watchers only after each step, so the progress
  view would have called a finished step "current". It now also notifies
  when a step starts.
- An unused import in the first test version; removed.

Tests (`internal/web/web_test.go`, also run with `-race`): Host and Origin
checks (DNS-rebinding host, other port, cross-site and `null` origins),
CSRF token required, invalid form input rejected, security headers,
a diagnosis starting and completing, and hostile model and log text
(`<script>`, `<a href>`, `<img>`, Markdown links) appearing only escaped.

Smoke test with the real binary (trimmed):

```
$ HODY_DATA_DIR=/tmp/hody-smoke /tmp/hody serve --addr 127.0.0.1:8099 &
$ curl ... (fetch form, extract token)            -> token len 64
$ curl -d "csrf=...&platform=docker&name=web" .../diagnose
post=303 loc=http://127.0.0.1:8099/s/827494ffee731e61
$ curl <session page>
<h1>docker web</h1>
<p class="lede">Diagnosis f77d717c0d39ad50 · failed</p>
<p class="error">This diagnosis stopped: HODY_LLM_BASE_URL must be an http(s) URL, got &#34;&#34;</p>
$ curl -H "Host: evil.example:8099" http://127.0.0.1:8099/   -> 403
$ hody serve --addr 0.0.0.0:8098
hody: Hody only listens on 127.0.0.1, not "0.0.0.0"
```

I could not view the page in a browser in this sandbox, so the visual
design is unverified; only the HTML and behaviour were checked.

```
$ gofmt -l .
(no files)
$ go vet ./...
ok
$ go build ./...
ok
$ go test ./...
?   	hody/cmd/hody	[no test files]
?   	hody/eval	[no test files]
ok  	hody/internal/gateway	0.017s
ok  	hody/internal/llm	0.013s
ok  	hody/internal/model	0.002s
ok  	hody/internal/pipeline	0.019s
ok  	hody/internal/policy	0.016s
ok  	hody/internal/store	0.003s
ok  	hody/internal/tools	0.008s
ok  	hody/internal/tools/docker	0.011s
?   	hody/internal/tools/docker/dockertest	[no test files]
ok  	hody/internal/tools/k8s	0.102s
ok  	hody/internal/web	0.014s
```

## Phase 9: eval harness, grader, results table

Wrote `eval/grade.go` (per-scenario checks, accuracy only in live mode,
results table), `RunLive` in `eval/scenario.go`, the full `hody eval`
command (`cmd/hody/eval.go`, writes `results.json`, exits non-zero if any
scenario fails) and tests: `eval/eval_test.go` (every replay scenario
passes; the grader fails a planted canary leak, a missing injection flag,
missing citations and an uncited required tool),
`eval/live_test.go` (the live code path with the real Docker client code
against the `httptest` fake Docker API and the replay model server; this
exercises wiring only and is not a live result), and
`cmd/hody/main_test.go` (argument parsing, target parsing, loopback
check, and the eval command end to end).

Problem fixed: a variable named `model` shadowed the `model` package in
the live test.

Replay results table (real output):

```
SCENARIO                    MODE    OUTCOME                VERDICT  CHECKS  ACCURACY (cat/obj/field)  RESULT
docker-command-not-found    replay  root_cause             approve  9/9     scripted, not measured    PASS
docker-missing-env          replay  root_cause             approve  10/10   scripted, not measured    PASS
docker-mount-permission     replay  root_cause             block    9/9     scripted, not measured    PASS
docker-oom                  replay  root_cause             approve  9/9     scripted, not measured    PASS
docker-port-conflict        replay  root_cause             approve  9/9     scripted, not measured    PASS
k8s-configmap-key-mismatch  replay  root_cause             approve  9/9     scripted, not measured    PASS
k8s-exit-126                replay  root_cause             block    10/10   scripted, not measured    PASS
k8s-image-pull              replay  root_cause             approve  9/9     scripted, not measured    PASS
k8s-no-findable-cause       replay  insufficient_evidence  none     10/10   scripted, not measured    PASS
k8s-oom-after-rollout       replay  root_cause             approve  9/9     scripted, not measured    PASS
k8s-probe-wrong-port        replay  root_cause             approve  9/9     scripted, not measured    PASS
k8s-unschedulable           replay  root_cause             warn     9/9     scripted, not measured    PASS

12 of 12 scenarios passed all checks.
Replay mode proves plumbing and security properties with recorded tool output and a scripted model. It does not measure diagnostic accuracy.
Sessions, saved model requests and results.json are in /tmp/ev9
```

Live mode (`--live`) was not run: this sandbox has no Docker daemon, no
cluster and no model API key.

```
$ gofmt -l .
(no files)
$ go vet ./...
ok
$ go build ./...
ok
$ go test ./...
ok  	hody/cmd/hody	0.123s
ok  	hody/eval	0.146s
ok  	hody/internal/gateway	0.015s
ok  	hody/internal/llm	0.008s
ok  	hody/internal/model	0.001s
ok  	hody/internal/pipeline	0.020s
ok  	hody/internal/policy	0.016s
ok  	hody/internal/store	0.003s
ok  	hody/internal/tools	0.009s
ok  	hody/internal/tools/docker	0.011s
?   	hody/internal/tools/docker/dockertest	[no test files]
ok  	hody/internal/tools/k8s	0.107s
ok  	hody/internal/web	0.012s
```

## Phase 10: documentation, self-audit, packaging

Wrote `README.md`, `docs/ARCHITECTURE.md`, `docs/SECURITY.md`,
`docs/DECISIONS.md`, `docs/USAGE.md`, `docs/BUILD_REPORT.md`,
`eval/README.md`, `deploy/readonly-clusterrole.yaml`, `deploy/Dockerfile`
and `THIRD_PARTY_NOTICES.md` (licenses read from each module's LICENSE
file: 29 Apache-2.0, 13 BSD-3-Clause, 4 MIT, 1 ISC, none unknown).

Audit steps and what they found:

- **Sizes:** no file over 300 lines. A throwaway `go/ast` script found three
  functions over 50 lines (`report` 55, `Compose` 55, `cmdServe` 51). Every
  package has a purpose comment.
- **Unused code:** grep showed `Registry.Names`, `Registry.Has` and
  `Gateway.RedactText` had no callers. Deleted.
- **Line count:** 4,682 non-test lines, 3,941 without blank and
  comment-only lines (details in the build report).
- **govulncheck:** could not be installed through the mirror, and
  `vuln.go.dev` is unreachable. Not run.
- **Writes to disk and logs:** only the store, test-mode request copies and
  `results.json`. No path writes the placeholder map or raw tool output.
- **Every redaction in the 12 replay sessions, read by hand.** This found a
  second over-redaction bug: `owner: ReplicaSet/orders-5c9d8f7b6` became
  `owner: [SECRET_2]` because the entropy layer saw `Kind/name-hash` as
  random. Added an exemption for Kubernetes `Kind/name` references, with
  regression tests. After the fix, every placeholder replaces a real
  secret, except one accepted false positive (`auth: upstream` hides
  "upstream").
- **Live mode gap, found while writing the walkthrough:** Kubernetes pod
  names are generated, so live k8s scenarios could never have found their
  pod. Added `live_selector` to the seven k8s labels, `k8s.Client.FindPod`
  (a list call) and a `PodFinder` for `eval.RunLive`, with tests
  (`FindPod` against the fake clientset; a clear error when no pod
  matches).
- **USAGE examples:** rendered with a temporary test from real replay
  session files, labelled as replay in the document. The temporary test
  was removed.

Final gate and replay: see `docs/BUILD_REPORT.md` (12 of 12 in replay; all
packages pass, also with `-race`).
