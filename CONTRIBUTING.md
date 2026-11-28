# Contributing to Hody

Thank you for your interest in contributing! This document explains how to get
started.

## Development setup

1. **Go 1.26+** — [install instructions](https://go.dev/doc/install)
2. Clone the repo:
   ```sh
   git clone https://github.com/astro-dally/Hody-AI.git
   cd Hody-AI
   ```
3. Build and test:
   ```sh
   go build -o hody ./cmd/hody
   go test ./...
   ```
4. Run the check script (format, vet, build, test):
   ```sh
   ./check.sh
   ```

## Before you submit

- [ ] `gofmt -l .` reports no files
- [ ] `go vet ./...` passes
- [ ] `go test -race ./...` passes
- [ ] All 12 eval scenarios pass: `./hody eval eval/scenarios`

CI (`.github/workflows/ci.yml`) runs all of these on every push and pull
request, on Linux and macOS with two Go versions, plus govulncheck, the
Docker image build, and the read-only ClusterRole on a kind cluster. A
pull request needs a green CI run.

## Code guidelines

- **Standard library first.** The only external dependency is `k8s.io/client-go`
  (and what it pulls in). No CLI frameworks, LLM SDKs, or database drivers.
- **Files under ~300 lines, functions under ~50 lines.**
- **Table-driven tests** for every rule, pattern, and validator.
- **Package-level purpose comments** in every package.
- **No interface without two real implementations.**
- **No global mutable state.**
- Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full lay of the
  land and [docs/DECISIONS.md](docs/DECISIONS.md) for why things are the way
  they are.

## Adding eval scenarios

See [eval/README.md](eval/README.md) for the full guide. Each scenario lives
in `eval/scenarios/<name>/` and needs:

- `label.json` — expected outcome, verdict, category, object, field
- `recorded/` — tool output files and an `index.json`
- `transcript.json` — scripted model answers
- `live/setup.sh` and `live/cleanup.sh` — for real-cluster testing

## Reporting issues

Open a GitHub issue with:
- What you expected vs. what happened
- Hody version (`git rev-parse --short HEAD`)
- Go version (`go version`)
- Steps to reproduce

## Security issues

If you find a security vulnerability, **do not open a public issue.** Email the
maintainers directly (see [SECURITY.md](docs/SECURITY.md)).

## License

By contributing, you agree that your contributions will be licensed under the
[MIT License](LICENSE).
