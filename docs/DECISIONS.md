# Decisions

Choices the specification left open, and why.

## Toolchain and dependencies

- **Go 1.26.8** (newest available to the build sandbox, from the Ubuntu
  archive). `go.mod` says `go 1.26.0`.
- **client-go v0.37.2**, the newest release tag at build time.
- **`sigs.k8s.io/yaml` imported directly** to render pod templates and
  manifests as YAML. It is already a client-go dependency, so no new module
  was added.
- **Module path `hody`.** A real hosting path was not known; a short local
  path avoids pretending to one. Rename it if you publish the code.
- **`go.sum` was produced through a local mirror** because the Go module
  proxy was blocked (see the build log). Five hashes matched known upstream
  values; the rest could not be checked against `sum.golang.org`.

## Architecture

- **Tool specs in `internal/tools`, bindings in the client packages**
  (`docker.Tools`, `k8s.Tools`). This makes the layering rule hold for the
  whole import graph, not just direct imports.
- **Scope lock by argument name.** Arguments named `namespace`, `container`
  and `path` must equal the session target. Every tool follows this naming.
  For Kubernetes, other pods, Deployments and ConfigMaps in the same
  namespace are allowed; the node tool takes the pod, not a node name, and
  reads only the node that pod runs on.
- **Docker container identity is the exact string the user gave.** If you
  diagnose `web`, a request for the container by its ID is rejected.
- **Pipeline steps are methods on a per-run `Engine`** holding the
  dependencies, with the shared signature
  `func(ctx context.Context, s *model.Session) error`.
- **The "approve" step completes the session** and records that it was
  presented. Hody has no separate "I approve" button because it never
  applies anything; approval is the user deciding what to do with the diff.
- **A step error fails the session.** That includes a model call failing
  after its retry, and the tripwire.
- **Tool failures become evidence** ("tool error: ..."), passed through the
  gateway, because failures are clues (for example "previous container not
  found").
- **The model chooses tool requests one at a time** (`request_tool` or
  `done`), so each request sees the result of the last.
- **What the fix targets:** the user's `--file` if given; otherwise a
  generated manifest. For Kubernetes it is the owning Deployment (or the
  bare pod) as YAML without status. For Docker it is a compose-style file
  generated from `docker inspect`. The diff is against the *redacted* text,
  so it may show `[SECRET_n]` placeholders.
- **Fixes are find/replace edits, and Hody builds the diff.** In live run 3,
  7 of 12 hand-written diffs did not match the manifest, though in most
  the model knew the one-line change; copying lines is far easier for a
  model than a diff's line numbers and markers. Each edit's find text must
  be complete lines that occur exactly once (constant indentation slips and
  a copied line-number column are tolerated); edits that do not apply get
  one retry with the reason. Hody builds the unified diff, and checks that
  applying it reproduces the edited text, so what the user sees is exactly
  what the rules judged.
- **Diff application** (now used to check the diff Hody builds) is exact on content but tolerates wrong hunk line
  numbers by searching near the stated line, and indentation that is off
  by the same number of spaces on every line of a hunk (the new lines are
  shifted by that amount, so the result keeps the file's real structure;
  the first live run showed a correct fix blocked for one extra space), and
  context lines written without their leading space marker. A fix may
  also have no diff when no change to the manifest can fix the cause
  (e.g. host file ownership): its manual steps get a warning and the
  critic's review, since the rules cannot check them.
  Only single-file diffs are accepted.
- **Conclusions are checked against the evidence, not just for valid IDs.**
  Live run 3's abstention scenario got an invented cause
  (`command_not_executable` while the evidence shows exit code 1). A known
  category whose signature appears nowhere in the evidence gets one retry,
  then a downgrade, but only when the evidence shows no known failure at
  all: when it shows a different one (live runs 2 and 3, `k8s-exit-126`:
  exit code 126, answered as `mount_permission`) the answer may be the
  same cause under a neighbouring name, so it is kept with a warning.
  Checked against all 31 conclusions of live runs 1 to 3 before the final
  run: only run 3's invented cause would be downgraded.
- **Policy rules match text**, so one rule set covers Kubernetes YAML,
  compose files, Dockerfiles and shell commands. A finding is raised only
  when the patch makes a rule worse: more matches for most rules, fewer
  for "removed limits" and "removed probes". Severity: block for
  privileged, root, host namespaces, host paths, capabilities, inline
  secrets, broad permissions and disabled TLS checks; warn for removed
  limits, removed probes and `latest` or untagged images.
- **Unknown verdicts count as block** when combining verdicts.
- **A failed critic call fails the run** rather than silently approving.
- **Endpoints are read from EndpointSlices** (the current API) under the
  `k8s.services` tool.

## Gateway parameters

- Limits: first 60 lines, last 80, up to 110 error-like lines from the
  middle; flagged lines are always kept; lines are cut at 400 bytes.
- Minimum secret length 4. Entropy threshold: tokens of 20+ characters
  with upper case, lower case and digits, and Shannon entropy at least
  `min(4.5, 0.85 × log2(length))` bits per character.
- Terminal escape sequences, control characters and bidirectional or
  zero-width characters are removed before scanning.
- The tripwire checks raw and JSON-escaped canaries. A blocked request is
  not saved, so a leaked canary is never written to disk; the error names
  only the canary's index.

## Model client

- `temperature: 0` and `response_format: {"type": "json_object"}` are
  sent. Most OpenAI-compatible servers, including Ollama, accept these.
- An `X-Hody-Purpose` header names the call's purpose. Real servers ignore
  it; the replay server uses it to pick the scripted answer.
- Every struct field without `omitempty` is required.

## Commands and storage

- Data directory: `HODY_DATA_DIR`, default `~/.hody`. Test-mode request
  copies go to `<data>/llm-requests/<session>/`.
- Test mode is switched on by `HODY_CANARIES` (comma-separated).
- Flags may come before or after the positional arguments, so
  `hody diagnose k8s pod/x -n ns` works with the standard `flag` package.
- `hody serve` accepts `--addr` but only with host `127.0.0.1`.
- Session IDs are 16 random hex characters; the store refuses anything else.

## Web page

- No JavaScript. The progress view refreshes every 2 seconds with a meta
  tag.
- CSRF protection uses a random per-process token in the form, compared in
  constant time, plus Origin checking.
- System fonts only, because the page's Content Security Policy allows no
  external resources.

## Evaluation

- Replay works at the tool registry level: recorded outputs stand in for
  tools. The fake clientset cannot return custom logs, so replay does not
  exercise the Docker and Kubernetes clients; those have their own tests
  against `httptest` and the fake clientset.
- Live mode never sets up scenarios. Each scenario's `live/setup.sh` is for
  a person to run, so that no code path in the binary changes a container
  or cluster.
- Accuracy (category, object, field) is reported only in live mode. Replay
  shows "scripted, not measured".
- The grader also checks scenario data: no canary may appear in a scripted
  model answer. A real model never sees raw secrets, so such an answer
  would be impossible.
