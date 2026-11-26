#!/bin/bash
# Hody live test: the server side (Ubuntu 24.04 on EC2).
# Run it inside tmux; "bash mac.sh ssh ..." puts you in tmux automatically.
#
#   bash ~/ec2.sh          install tools, build, run every check, then the full live eval
#   bash ~/ec2.sh setup    only install the tools
#   bash ~/ec2.sh smoke    stop after the one-scenario smoke test (docker-oom)
#   bash ~/ec2.sh api      use a hosted API (free key) instead of Ollama on your Mac
#
# The model is reached through llm-proxy.py (copied here by mac.sh), which
# retries rate-limit and connection errors and removes <think> blocks.
# Safe to run again: anything already done is skipped.
set -Eeuo pipefail
MODE=${1:-all}
case $MODE in all|setup|smoke|api) ;; *) sed -n '5,13p' "$0" | sed 's/^# \{0,1\}//'; exit 1 ;; esac

REPO_URL=${REPO_URL:-https://github.com/astro-dally/Hody-AI.git}
REPO_DIR=${REPO_DIR:-$HOME/Hody-AI}
# Images the scenarios use, pulled once up front (the image-pull scenario's typo tag is meant to fail).
DOCKER_IMAGES="python:3.12-slim node:22-alpine nginx:1.27 busybox:1.36"
export PATH="$PATH:/snap/bin:$HOME/go/bin"
PROXY="$(cd "$(dirname "$0")" && pwd)/llm-proxy.py"
[ -f "$PROXY" ] || PROXY="$HOME/llm-proxy.py"
PROXY_URL=http://127.0.0.1:11500

say()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m!!  %s\033[0m\n' "$*"; }
die()  { printf '\n\033[1;31mSTOP: %s\033[0m\n' "$*" >&2; exit 1; }

install_tools() {
  if ! command -v docker >/dev/null || ! command -v tmux >/dev/null || ! command -v gcc >/dev/null ||
     ! command -v fuser >/dev/null || ! command -v unzip >/dev/null; then
    say "Installing system packages (a few minutes the first time)"
    sudo apt-get update -q
    sudo DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a \
      apt-get install -y -q docker.io git curl python3 unzip tmux gcc psmisc
  fi
  sudo systemctl enable --now docker >/dev/null 2>&1
  id -nG "$USER" | grep -qw docker || sudo usermod -aG docker "$USER"
  command -v go >/dev/null      || { say "Installing Go";      sudo snap install go --classic; }
  command -v kubectl >/dev/null || { say "Installing kubectl"; sudo snap install kubectl --classic; }
  command -v kind >/dev/null    || { say "Installing kind";    go install sigs.k8s.io/kind@latest; }
  grep -q 'HOME/go/bin' ~/.bashrc || echo 'export PATH=$PATH:$HOME/go/bin' >> ~/.bashrc

  minor=$(go env GOVERSION | sed -E 's/^go1\.([0-9]+).*/\1/')
  [ "$minor" -ge 26 ] || die "Go $(go env GOVERSION) is too old; Hody needs 1.26+. Run: sudo snap refresh go --classic"
}

docker_access() {
  docker info >/dev/null 2>&1 && return
  if [ -z "${HODY_REEXEC:-}" ] && getent group docker | grep -qw "$USER"; then
    say "Picking up the new docker group (no need to log out)"
    exec sg docker -c "HODY_REEXEC=1 bash $(printf %q "$0") $MODE"
  fi
  die "can't talk to Docker. Type exit, SSH back in, and run this again."
}

start_proxy() {  # upstream, key, model, seconds between requests, 1 if Ollama
  PX_UP=$1; PX_KEY=$2; PX_MODEL=$3; PX_GAP=$4; PX_NATIVE=${5:-}
  [ -f ~/.hody-llm-proxy.pid ] && kill "$(cat ~/.hody-llm-proxy.pid)" 2>/dev/null && sleep 0.5
  rm -f "$HOME/hody-llm-usage.json"
  UPSTREAM="$PX_UP" API_KEY="$PX_KEY" MODEL="$PX_MODEL" MIN_INTERVAL="$PX_GAP" OLLAMA_NATIVE="$PX_NATIVE" MAX_WAIT=300 UPSTREAM_TIMEOUT=600 \
    nohup python3 "$PROXY" >>"$HOME/hody-llm-proxy.log" 2>&1 &
  echo $! > ~/.hody-llm-proxy.pid
  for _ in $(seq 1 20); do curl -fs -o /dev/null "$PROXY_URL/health" && return; sleep 0.5; done
  die "the model helper didn't start. See ~/hody-llm-proxy.log"
}

setup_api() {
  echo "Which provider?  1) Google Gemini   2) Mistral   3) Groq   4) other OpenAI-compatible"
  read -r -p "Number: " n
  case $n in
    1) up=https://generativelanguage.googleapis.com/v1beta/openai; m=gemini-2.5-flash; gap=7 ;;
    2) up=https://api.mistral.ai/v1; m=mistral-small-latest; gap=1.5 ;;
    3) up=https://api.groq.com/openai/v1; m=llama-3.3-70b-versatile; gap=3 ;;
    4) read -r -p "Base URL (the part before /chat/completions): " up; m=""; gap=2 ;;
    *) die "type a number from 1 to 4" ;;
  esac
  read -r -p "Model name [$m]: " x; m=${x:-$m}
  [ -n "$m" ] || die "a model name is needed"
  read -r -s -p "API key (hidden while you type): " k; echo
  [ -n "$k" ] || die "no key entered"
  ( umask 077; printf 'LLM_UPSTREAM=%q\nLLM_KEY=%q\nLLM_MODEL=%q\nLLM_GAP=%q\n' "$up" "$k" "$m" "$gap" > ~/.hody-llm )
  say "Saved in ~/.hody-llm (only you can read it). To go back to Ollama on your Mac: rm ~/.hody-llm"
}

check_model() {
  [ -f "$PROXY" ] || die "llm-proxy.py is missing. On the Mac, run 'bash scripts/mac.sh ssh ...' again; it copies it here."
  if [ -f ~/.hody-llm ]; then
    # shellcheck disable=SC1090
    . ~/.hody-llm
    LLM_HOW="the hosted API at $LLM_UPSTREAM"
    say "Using $LLM_HOW (model $LLM_MODEL)"
    start_proxy "$LLM_UPSTREAM" "$LLM_KEY" "$LLM_MODEL" "$LLM_GAP"
    HODY_LLM_MODEL=$LLM_MODEL
  else
    LLM_HOW="Ollama ($(cat ~/.hody-ollama-where 2>/dev/null || echo 'on the Mac')), through an SSH tunnel"
    say "Using $LLM_HOW"
    models=$(curl -fsS --max-time 10 http://localhost:11434/v1/models) ||
      die "can't reach Ollama. On your Mac, is 'bash scripts/mac.sh up ...' still running? Is the Mac awake (and Tailscale on, for the GPU machine)?"
    want=${HODY_LLM_MODEL:-$(cat ~/.hody-model 2>/dev/null || true)}
    HODY_LLM_MODEL=$(MODELS="$models" WANT="$want" python3 -c '
import json, os, sys
ids = [m["id"] for m in json.loads(os.environ["MODELS"]).get("data", [])]
want = os.environ["WANT"]
if want in ids: print(want)
elif not want and ids: print(ids[0])
else: sys.exit("model %r not found; the Mac has: %s" % (want, ", ".join(ids) or "none"))
') || die "pick a model the Mac has (see above)"
    start_proxy http://localhost:11434 "" "$HODY_LLM_MODEL" 0 1
  fi
  export HODY_LLM_MODEL HODY_LLM_BASE_URL="$PROXY_URL/v1" HODY_LLM_API_KEY=""
  export HODY_LLM_TIMEOUT=${HODY_LLM_TIMEOUT:-1200}
  say "Asking $HODY_LLM_MODEL a test question in JSON mode (the first answer can take a minute)"
  start=$(date +%s)
  answer=$(curl -fsS --max-time 600 "$HODY_LLM_BASE_URL/chat/completions" -H 'Content-Type: application/json' \
    -d "{\"model\":\"$HODY_LLM_MODEL\",\"temperature\":0,\"response_format\":{\"type\":\"json_object\"},\"messages\":[{\"role\":\"user\",\"content\":\"Reply with only this JSON object: {\\\"status\\\": \\\"ready\\\"}\"}]}") ||
    die "the model didn't answer. Details: tail ~/hody-llm-proxy.log"
  content=$(printf '%s' "$answer" | python3 -c 'import json,sys; print(json.load(sys.stdin)["choices"][0]["message"]["content"].strip())')
  echo "   answered in $(( $(date +%s) - start ))s: ${content:0:80}"
  case $content in
    "{"*) ;;
    *) warn "The answer doesn't start with '{', so Hody would reject it. Tell Claude what it printed." ;;
  esac
}

get_code() {
  if [ -d "$REPO_DIR/.git" ]; then
    say "Updating the code (git pull)"
    git -C "$REPO_DIR" pull --ff-only || die "git pull failed. See what changed with: git -C $REPO_DIR status"
  else
    say "Downloading the code from $REPO_URL"
    git clone -q "$REPO_URL" "$REPO_DIR" || die "git clone failed. If the repo is private, make it public or set REPO_URL."
  fi
  cd "$REPO_DIR"
  echo "   commit: $(git log -1 --format='%h %s')"
  [ -f eval/run-live.sh ] ||
    die "this version of Hody has no eval/run-live.sh. Push the live-run changes to GitHub first, then run this again."
}

offline_checks() {
  say "Offline checks: verify modules, build, tests, replay eval, dry run"
  go mod verify
  go build -o hody ./cmd/hody
  bash check.sh
  ./hody eval eval/scenarios
  DRY_RUN=1 bash eval/run-live.sh
}

prepare_cluster() {
  k8s_images=$(grep -ho 'image: *[^ ]*' eval/scenarios/*/live/*.yaml | awk '{print $2}' | sort -u)
  say "Downloading the scenario images once"
  for img in $(printf '%s\n' $DOCKER_IMAGES $k8s_images | sort -u); do
    docker image inspect "$img" >/dev/null 2>&1 && continue
    docker pull -q "$img" >/dev/null 2>&1 && echo "   $img" ||
      echo "   $img could not be pulled (expected for the deliberate typo nginx:1.27.O; otherwise try 'docker login')"
  done
  if ! kind get clusters 2>/dev/null | grep -qx kind; then
    say "Creating the kind cluster (a couple of minutes)"
    kind create cluster --wait 180s
  fi
  kubectl config use-context kind-kind >/dev/null
  kubectl get nodes
  say "Loading the images into the cluster, so pods don't download them again"
  for img in $k8s_images; do
    docker image inspect "$img" >/dev/null 2>&1 || continue
    kind load docker-image "$img" >/dev/null 2>&1 && echo "   $img" || echo "   $img (load failed; the cluster will download it itself)"
  done
}

newest_result() { ls -td eval/results/live-* 2>/dev/null | head -n1 || true; }

smoke_test() {
  say "Smoke test: one scenario (docker-oom)"
  rc=0; bash eval/run-live.sh docker-oom || rc=$?
  r=$(newest_result); [ -n "$r" ] && sed -n '1,15p' "$r/summary.md"
  [ "$rc" -eq 0 ] && return
  warn "The smoke test did not pass every check (exit code $rc). Details: $REPO_DIR/$r/docker-oom/"
  [ "$MODE" = smoke ] && exit 1
  read -r -p "Run the full test anyway? [y/N] " a
  [ "$a" = y ] || exit 1
}

full_run() {
  say "Full live run: all 12 scenarios (roughly 30-60 minutes). You can detach: Ctrl-b, then d."
  start_proxy "$PX_UP" "$PX_KEY" "$PX_MODEL" "$PX_GAP" "$PX_NATIVE"   # fresh usage counters for this run
  t0=$(date +%s)
  rc=0; bash eval/run-live.sh || rc=$?
  r=$(newest_result)
  [ -n "$r" ] && [ -f "$r/summary.md" ] || die "the run produced no summary.md (exit code $rc)"
  cp "$HOME/hody-llm-usage.json" "$r/llm-usage.json" 2>/dev/null || true
  cp "$HOME/hody-llm-proxy.log" "$r/llm-proxy.log" 2>/dev/null || true
  cp "$r/summary.md" ~/LIVE_EVAL.md
  HOW="$LLM_HOW" python3 -c '
import json, os, sys
try:
    u = json.load(open(sys.argv[1]))
except (OSError, ValueError):
    u = {}
print("\n## How the model was reached\n")
print("Model `%s`, via %s. Requests passed through `scripts/llm-proxy.py`, which retries "
      "rate-limit and connection errors and removes a leading `<think>` block from answers; "
      "it does not change what is sent to the model." % (u.get("model", "?"), os.environ["HOW"]))
if u:
    print("\nModel usage for this run: %d requests, %d prompt tokens, %d completion tokens, "
          "%d retries, %d failed requests, %d think blocks removed."
          % (u["requests"], u["prompt_tokens"], u["completion_tokens"], u["retries"],
             u["failed"], u.get("think_blocks_removed", 0)))
' "$r/llm-usage.json" >> ~/LIVE_EVAL.md
  tar czf ~/hody-live-results.tgz -C "$(dirname "$r")" "$(basename "$r")"
  say "Done in $(( ($(date +%s) - t0) / 60 )) minutes. Summary:"
  sed -n '1,30p' "$r/summary.md"
  echo
  echo "   Full results: $REPO_DIR/$r"
  echo "   On your Mac:  bash mac.sh fetch <key.pem> <server-ip>"
  echo "   Then here:    kind delete cluster    and stop the instance in the AWS console."
}

install_tools
docker_access
LOG="$HOME/hody-$(date +%Y%m%d-%H%M%S).log"
exec > >(tee -a "$LOG") 2>&1
trap 'printf "\n\033[1;31mSTOP: the step above failed. Full log: %s\033[0m\n" "$LOG"' ERR
echo "Log file: $LOG"
{ docker --version; go version; kind version; kubectl version --client 2>/dev/null | head -n1; } || true

if [ "$MODE" = api ]; then
  setup_api
  check_model
  say "The API works. Next: bash ~/ec2.sh smoke"
  exit 0
fi
if [ "$MODE" = setup ]; then
  say "Tools are ready. Next: bash ~/ec2.sh"
  exit 0
fi
check_model
get_code
offline_checks
prepare_cluster
smoke_test
[ "$MODE" = smoke ] && { say "Smoke test passed. Next: bash ~/ec2.sh"; exit 0; }
full_run
