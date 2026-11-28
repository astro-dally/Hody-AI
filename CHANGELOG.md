# Changelog

All notable changes to Hody are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-11-28

The first release: the version measured in the final live evaluation
(`docs/LIVE_EVAL.md`), plus the commands and packaging below.

### Diagnosis

- `hody diagnose docker <container>` and `hody diagnose k8s pod/<name>`:
  collect status, logs, events and configuration through read-only tools;
  triage with code rules; let the model request up to 8 more read-only
  tools; conclude with cited evidence or "not enough evidence"; propose a
  fix; review it.
- Secrets are redacted to `[SECRET_n]` before the model sees anything, and
  a canary tripwire stops a run if one ever gets through.
- Lines that look like prompt injection are flagged and passed as quoted
  data.
- Conclusions are checked in code against the evidence: cited IDs must
  exist, a known failure's signature must appear and be cited, and the
  evidence naming the faulty setting should be cited.
- Fixes are find/replace edits; Hody applies them to a copy of the
  manifest, builds the diff, and reviews it with 11 policy rules and a
  separate critic model, the stricter verdict winning. Nothing is applied.
- `hody serve`: a local web page (127.0.0.1 only).

### Using it

- `hody` at a terminal says hello and lists the commands; every command
  has `--help`.
- `hody doctor` checks Docker, Kubernetes and the model server, and says
  what to fix. It only reads and sends nothing to a model.
- `hody version`. The Docker socket of Docker Desktop, Colima, OrbStack
  and Rancher Desktop is found on its own.

### Evaluation

- 12 scenarios (5 Docker, 7 Kubernetes) with planted canaries and
  injection lines; replay mode and a live runner. Final live run with
  `qwen3:32b`: 11 of 12 passed every check, 11 of 11 fixes applied, no
  secret in 57 model requests (194 over four runs).

### Packaging

- Binaries for macOS, Linux and Windows (amd64, arm64) with checksums;
  a Homebrew cask; a multi-arch image on GitHub Container Registry (and
  Docker Hub when configured).
- CI on every push: tests on Linux and macOS with two Go versions, the
  race detector, the replay evaluation, govulncheck, the Docker image,
  and the read-only ClusterRole on a kind cluster.

[0.1.0]: https://github.com/astro-dally/Hody-AI/releases/tag/v0.1.0
