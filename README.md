<p align="center">
  <h1 align="center">🔍 Hody</h1>
  <p align="center">
    <strong>AI-powered container failure diagnosis that keeps your secrets safe.</strong>
  </p>
  <p align="center">
    <a href="https://github.com/astro-dally/Hody-AI/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/astro-dally/Hody-AI/actions/workflows/ci.yml/badge.svg"></a>
    <a href="https://github.com/astro-dally/Hody-AI/releases"><img alt="Release" src="https://img.shields.io/github/v/release/astro-dally/Hody-AI?sort=semver"></a>
    <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/astro-dally/Hody-AI"></a>
    <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/github/license/astro-dally/Hody-AI"></a>
  </p>
  <p align="center">
    <a href="#quick-start">Quick Start</a> •
    <a href="#how-it-works">How It Works</a> •
    <a href="docs/USAGE.md">Walkthroughs</a> •
    <a href="docs/ARCHITECTURE.md">Architecture</a> •
    <a href="docs/SECURITY.md">Security</a>
  </p>
</p>

---

Hody tells you **why a Docker container or Kubernetes pod failed** — without sending your passwords and keys to the AI model.

When a container crashes, it's tempting to paste logs into a chatbot. But those logs often contain secrets, the chatbot may hallucinate, and its advice can be dangerous ("just run it as root"). Hody does the same job more carefully, with **five promises**:

| # | Promise | How |
|---|---------|-----|
| 🔒 | **Secrets never reach the AI** | Passwords, tokens & keys are replaced with `[SECRET_n]` placeholders before the model sees anything |
| 🛡️ | **Hard to trick** | Injected instructions in logs are flagged; the model can only call read-only tools within your chosen scope |
| 📎 | **Every claim cites evidence** | Code checks that cited evidence exists and shows the claimed kind of failure; unsupported claims are downgraded |
| ✅ | **Fixes are safety-checked** | 11 policy rules + a separate AI critic review every proposed fix before you see it |
| 🤷 | **Says "not enough evidence"** | Instead of guessing when it doesn't know |

> **Hody only reads.** It never changes your containers, cluster, or images. It shows you a diff and explains it — you decide.

## Requirements

| Requirement | Details |
|-------------|---------|
| **Container runtime** | Docker (for Docker diagnoses) or a Kubernetes cluster (for K8s diagnoses). [kind](https://kind.sigs.k8s.io/) works well for testing |
| **AI model** | Any OpenAI-compatible API, including free local models through [Ollama](https://ollama.com/). The live evaluation used `qwen3:32b` and `qwen3:30b`; smaller models are untested |
| **Go** | 1.26+, only to build from source ([install](https://go.dev/doc/install)) |

## Quick Start

### Install

**macOS (Homebrew):**
```sh
brew install astro-dally/tap/hody
```

If macOS ever says it "cannot verify" hody (the binaries are not notarised
by Apple), run `xattr -dr com.apple.quarantine "$(which hody)"` once.

**macOS, Linux or Windows (download):** pick your platform on the
[Releases page](https://github.com/astro-dally/Hody-AI/releases), check
the file against `checksums.txt`, unpack it, and put `hody` on your PATH.

**Container image** (mainly for Kubernetes and CI; see [Deployment](#deployment)):
```sh
docker pull ghcr.io/astro-dally/hody
```

**From source:**
```sh
git clone https://github.com/astro-dally/Hody-AI.git && cd Hody-AI
go build -o hody ./cmd/hody
```

### First run

```sh
hody            # says hello and lists the commands
hody doctor     # checks Docker, Kubernetes and the AI model, and says what to fix
```

### Configure the model

```sh
# Local model with Ollama (free; start it with OLLAMA_CONTEXT_LENGTH=16384 ollama serve)
ollama pull qwen3:32b
export HODY_LLM_BASE_URL=http://localhost:11434/v1
export HODY_LLM_MODEL=qwen3:32b
export HODY_LLM_API_KEY=          # leave empty for Ollama

# Or any OpenAI-compatible hosted API
export HODY_LLM_BASE_URL=https://api.example.com/v1
export HODY_LLM_MODEL=your-model
export HODY_LLM_API_KEY=sk-...

# Optional: seconds one model request may take (default 180; raise it for slow local models)
export HODY_LLM_TIMEOUT=600
```

Diagnoses are saved under `~/.hody` (override with `HODY_DATA_DIR`).

### Diagnose

**Docker container:**
```sh
hody diagnose docker my-container
```

**Kubernetes pod:**
```sh
hody diagnose k8s pod/api-7d9f8b6c5d-x2k9p -n shop
```

**Web UI** (localhost only):
```sh
hody serve            # → http://127.0.0.1:8080
```

**Replay mode** (no Docker, cluster, or model needed; from a clone of the repo):
```sh
hody eval eval/scenarios
```

> Add `--file path/to/deployment.yaml` to get fixes against your own manifest instead of the live object. Every command has `--help`.

## How It Works

Hody runs a **7-step pipeline** in a fixed order:

```
  ┌─────────┐   ┌────────┐   ┌─────────────┐   ┌──────────┐
  │ Collect  │──▶│ Triage │──▶│ Investigate │──▶│ Conclude │
  └─────────┘   └────────┘   └─────────────┘   └──────────┘
       │                            │                 │
       ▼                            ▼                 ▼
  Status, logs,              Model requests       Root cause +
  events, config             read-only tools      cited evidence
                             (max 8, validated)

  ┌─────────────┐   ┌────────────┐   ┌─────────┐
  │ Propose Fix │──▶│ Review Fix │──▶│ Approve │
  └─────────────┘   └────────────┘   └─────────┘
       │                  │                │
       ▼                  ▼                ▼
  Line edits →       11 policy rules    You see the
  diff built by Hody  + AI critic        result — nothing
                     (stricter wins)     is ever applied
```

**Everything flows through the Gateway** — a single function that redacts secrets, flags injection attempts, limits output size, numbers evidence, and wraps it in tamper-resistant delimiters. Raw text never leaves this function.

For a deep dive, see [Architecture](docs/ARCHITECTURE.md).

## Example Output

```
Hody diagnosis 2fd85b4a28f25cd1 for docker report-worker: complete

Root cause (oom_killed): The worker process needs more memory than the
64 MiB limit while building the report, so the kernel kills it
(OOMKilled, exit code 137).

Proposed fix (not applied):
--- a/docker-compose.yml
+++ b/docker-compose.yml
@@ -10,1 +10,1 @@
-    mem_limit: 67108864
+    mem_limit: 536870912

Review verdict: APPROVE (policy: approve, critic: approve)

1 log line(s) looked like instructions aimed at an AI;
they were treated as data only.
```

## Eval Scenarios

Hody ships with **12 built-in scenarios** covering common failures:

| Scenario | Platform | Failure |
|----------|----------|---------|
| `docker-oom` | Docker | Out of memory kill |
| `docker-command-not-found` | Docker | Bad entrypoint |
| `docker-port-conflict` | Docker | Port already in use |
| `docker-mount-permission` | Docker | Volume permission denied |
| `docker-missing-env` | Docker | Missing environment variable |
| `k8s-oom-after-rollout` | K8s | OOM after deployment |
| `k8s-image-pull` | K8s | Image pull failure |
| `k8s-exit-126` | K8s | Permission denied in container |
| `k8s-unschedulable` | K8s | Insufficient resources |
| `k8s-probe-wrong-port` | K8s | Liveness probe misconfigured |
| `k8s-configmap-key-mismatch` | K8s | Wrong ConfigMap key reference |
| `k8s-no-findable-cause` | K8s | Abstention (no clear cause) |

Run them all in replay mode (recorded tool output and a scripted model; proves the plumbing and safety checks, not accuracy):
```sh
./hody eval eval/scenarios
# 12 of 12 scenarios passed all checks.
```

Run them live, against real broken containers and pods and a real model. This is the only mode that measures diagnostic accuracy:
```sh
kind create cluster                 # for the Kubernetes scenarios
bash eval/run-live.sh               # one scenario at a time: set up, wait for the failure, diagnose, clean up
cat eval/results/live-*/summary.md
```

**Recorded live result** ([docs/LIVE_EVAL.md](docs/LIVE_EVAL.md), qwen3:32b, real Docker and Kubernetes): 11 of 12 scenarios passed every check; category and object right in 11 of 11, field in 10 of 11; all 11 proposed edits applied cleanly to the manifest (up from 4 of 11 with hand-written diffs on the same model); no planted secret in any of the 57 requests sent to the model; all 12 injection lines flagged. The miss: the "not enough evidence" scenario still got an invented cause. Across four runs, 194 model requests carried no planted secret, and Hody's deterministic checks caught bad fixes the AI critic approved. [docs/BUILD_REPORT.md](docs/BUILD_REPORT.md) explains every result, including what failed.

[docs/LIVE_TEST_GUIDE.md](docs/LIVE_TEST_GUIDE.md) walks through a complete live run on an AWS EC2 server, step by step. See [eval/README.md](eval/README.md) for how scenarios work and how to add your own.

## Security Model

Hody's security is **defense in depth**, not a single barrier:

- **Secret redaction** — 3 layers: key-name matching, known formats (AWS keys, JWTs, GitHub tokens, etc.), and entropy analysis
- **Injection scanning** — 9 heuristics flag attempts to manipulate the AI via log content
- **Read-only enforcement** — Docker client allows only GET; K8s client allows only get/list/log; source-level tests verify this
- **Scope lock** — the model can only request tools for the specific container/namespace you chose
- **Policy rules** — 11 rules block dangerous fixes (privileged mode, root user, host networking, inline secrets, etc.)
- **Independent critic** — a second model call reviews fixes without seeing any log evidence
- **Web hardening** — localhost-only, Host/Origin validation, CSRF tokens, strict CSP

> These are design goals backed by 99 test functions, not guarantees. See [docs/SECURITY.md](docs/SECURITY.md) for the full threat model and known limits.

## Deployment

### Container image

```sh
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e HODY_LLM_BASE_URL -e HODY_LLM_API_KEY -e HODY_LLM_MODEL \
  ghcr.io/astro-dally/hody diagnose docker <container>
```

Be aware of what the socket mount means: whoever holds Docker's socket
controls Docker, and adding `:ro` does not change that (it makes the
socket file read-only, not the Docker API). Hody's own client only sends
GET requests to an allowlist of paths, which is enforced and tested, but
on your own machine the installed binary is the simpler, safer choice.
The image suits Kubernetes and CI, where it needs no socket at all.

### Kubernetes RBAC

Apply the minimal read-only ClusterRole, then bind it in each namespace
Hody may read:
```sh
kubectl apply -f deploy/readonly-clusterrole.yaml
bash deploy/check-readonly-rbac.sh   # on a throwaway cluster: checks it allows reads and nothing else
```
CI runs that check on a kind cluster for every push.

### Releases

Tagging `v*` builds and publishes everything; see
[docs/RELEASING.md](docs/RELEASING.md).

## Project Structure

```
cmd/hody/              CLI commands (serve, diagnose, doctor, eval, version, report)
internal/
├── model/             Data types (Session, Evidence, RootCause, etc.)
├── gateway/           Redact → Scan → Limit → Number → Wrap
├── llm/               OpenAI-compatible client, tripwire, replay
├── pipeline/          7-step pipeline (one file per step)
├── policy/            11 fix safety rules + diff application
├── store/             Session JSON + audit log
├── tools/             Tool registry, validation, scope lock
│   ├── docker/        Docker read-only client + tool bindings
│   └── k8s/           Kubernetes read-only client + tool bindings
└── web/               Localhost web UI (stdlib, no JS framework)
eval/                  12 scenarios, harness, grader, run-live.sh (live runner)
scripts/               Live-test helpers: mac.sh, ec2.sh, llm-proxy.py
deploy/                Dockerfile + read-only ClusterRole
docs/                  Architecture, usage, security, build report, live test guide
```

## Documentation

| Document | Description |
|----------|-------------|
| [docs/USAGE.md](docs/USAGE.md) | Full walkthroughs for Docker & Kubernetes, Ollama setup |
| [docs/LIVE_TEST_GUIDE.md](docs/LIVE_TEST_GUIDE.md) | Step-by-step live evaluation on AWS EC2 with the `scripts/` helpers |
| [docs/LIVE_EVAL.md](docs/LIVE_EVAL.md) | The recorded live run; earlier runs in `docs/live-runs/` |
| [docs/RELEASING.md](docs/RELEASING.md) | How releases are built and published, and the one-time setup |
| [CHANGELOG.md](CHANGELOG.md) | What changed in each release |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Pipeline internals, gateway, layering rules |
| [docs/SECURITY.md](docs/SECURITY.md) | Threat model, what's covered, what's not |
| [docs/BUILD_REPORT.md](docs/BUILD_REPORT.md) | Complete build verification & test results |
| [docs/DECISIONS.md](docs/DECISIONS.md) | Design decisions and rationale |
| [eval/README.md](eval/README.md) | Scenario format & how to add new ones |
| [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) | Third-party dependency licenses |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to contribute |

## Tech Stack

- **Language:** Go 1.26 (stdlib-first, single binary)
- **Dependencies:** Only `k8s.io/client-go` and its transitive deps — no CLI frameworks, LLM SDKs, or database drivers
- **AI:** Any OpenAI-compatible API (local or hosted)
- **Testing:** 99 test functions, table-driven, race-detector clean; 12 scenarios in replay and live mode; CI on Linux and macOS with govulncheck and a kind cluster
- **Container:** Distroless non-root image

## License

[MIT](LICENSE)

---

<p align="center">
  <sub>Built with care for security. Not a guarantee — a serious effort.</sub>
</p>
