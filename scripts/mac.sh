#!/bin/bash
# Hody live test: the Mac side.
#
#   GPU=<user>@<gpu-machine> bash mac.sh up <key.pem> <server-ip>   use Ollama on your GPU machine (via Tailscale)
#   bash mac.sh up    <key.pem> <server-ip>    use Ollama on this Mac instead
#   bash mac.sh ssh   <key.pem> <server-ip>    open a terminal on the server (inside tmux)
#   bash mac.sh fetch <key.pem> <server-ip>    download the results to ~/Downloads
#
# "up" keeps running, because it IS the connection. Leave its window open; Ctrl-C stops it.
# Optional: MODEL=<name> to choose the model (default for GPU mode: qwen3:30b).
set -euo pipefail

MODEL=${MODEL:-}
GPU=${GPU:-}
GPU_SOCK="/tmp/hody-gpu-$(id -un).sock"

# Use the Mac own curl: a MacPorts or Homebrew copy earlier in PATH may be built for another CPU.
[ -x /usr/bin/curl ] && curl() { /usr/bin/curl "$@"; }

say()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m!!  %s\033[0m\n' "$*"; }
die()  { printf '\n\033[1;31mSTOP: %s\033[0m\n' "$*" >&2; exit 1; }
usage() { sed -n '4,10p' "$0" | sed 's/^# \{0,1\}//'; exit 1; }

cmd=${1:-}; key=${2:-}; ip=${3:-}
[ -n "$cmd" ] && [ -n "$key" ] && [ -n "$ip" ] || usage
[ -f "$key" ] || die "key file not found: $key"
chmod 400 "$key"
here=$(cd "$(dirname "$0")" && pwd)
server="ubuntu@$ip"
ssh_opts=(-i "$key" -o ServerAliveInterval=30 -o ServerAliveCountMax=4
          -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15)

copy_scripts() {
  for f in ec2.sh llm-proxy.py; do [ -f "$here/$f" ] || die "$f must be in the same folder as mac.sh"; done
  scp -q "${ssh_opts[@]}" "$here/ec2.sh" "$here/llm-proxy.py" "$server:~/" ||
    die "can't reach $server. Check the IP (it changes after every stop/start) and that the security group allows SSH from your current IP."
}

# ── Model on the GPU machine ────────────────────────────────────────────────
open_gpu() {  # forward this Mac's port 11435 to Ollama on the GPU machine
  ssh -S "$GPU_SOCK" -O check "$GPU" >/dev/null 2>&1 && return 0
  rm -f "$GPU_SOCK"
  ssh -f -N -M -S "$GPU_SOCK" -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 \
      -o ServerAliveCountMax=3 -o ConnectTimeout=15 -L 11435:localhost:11434 "$GPU"
}

setup_gpu() {
  say "Connecting to the GPU machine ($GPU) over Tailscale"
  open_gpu || die "can't SSH to $GPU. Is Tailscale on (on this Mac and the GPU machine)? Does 'ssh $GPU' work on its own?"
  tags=$(curl -fsS --max-time 10 localhost:11435/api/tags) ||
    die "connected, but Ollama isn't answering on the GPU machine. Start it there (ollama serve, or open the Ollama app)."
  MODEL=${MODEL:-qwen3:30b}
  TAGS="$tags" WANT="$MODEL" python3 -c '
import json, os, sys
names = [m["name"] for m in json.loads(os.environ["TAGS"]).get("models", [])]
if os.environ["WANT"] not in names:
    sys.exit("model %s is not on the GPU machine; it has: %s" % (os.environ["WANT"], ", ".join(names) or "none"))
' || die "pick one of those with MODEL=<name>"
  WHERE="on the GPU machine, reached over Tailscale"
  LPORT=11435
}

# ── Model on this Mac ───────────────────────────────────────────────────────
setup_local() {
  command -v ollama >/dev/null || die "Ollama isn't installed on this Mac. Use GPU=... for your GPU machine, or install it from https://ollama.com"
  if ! curl -s -o /dev/null localhost:11434; then
    say "Starting Ollama"
    nohup ollama serve >"$HOME/hody-ollama.log" 2>&1 &
    ollama_pid=$!
    for _ in $(seq 1 30); do curl -s -o /dev/null localhost:11434 && break; sleep 1; done
  fi
  if [ -z "$MODEL" ]; then
    for m in qwen3:30b qwen2.5:14b llama3.1:8b; do ollama show "$m" >/dev/null 2>&1 && { MODEL=$m; break; }; done
  fi
  if [ -z "$MODEL" ]; then
    gb=$(( $(sysctl -n hw.memsize) / 1073741824 ))
    if [ "$gb" -ge 32 ]; then MODEL=qwen2.5:14b; else MODEL=llama3.1:8b; fi
  fi
  ollama show "$MODEL" >/dev/null 2>&1 || { say "Downloading $MODEL"; ollama pull "$MODEL"; }
  WHERE="on the Mac"
  LPORT=11434
}

# ── Keep everything running ─────────────────────────────────────────────────
ollama_pid=""; tun_pid=""
stop() {
  rc=$?
  trap - INT TERM EXIT
  echo; say "Closing the connections"
  [ -n "$tun_pid" ] && kill "$tun_pid" 2>/dev/null
  [ -n "$GPU" ] && ssh -S "$GPU_SOCK" -O exit "$GPU" >/dev/null 2>&1
  [ -n "$ollama_pid" ] && kill "$ollama_pid" 2>/dev/null
  exit "$rc"
}

up() {
  [ "$(uname)" = Darwin ] || die "run this on your Mac"
  say "Copying ec2.sh and llm-proxy.py to the server"
  copy_scripts
  trap stop INT TERM EXIT
  if [ -n "$GPU" ]; then setup_gpu; else setup_local; fi
  say "Model: $MODEL ($WHERE)"

  say "Asking the model a test question with a 16k context window (loading it can take a minute)"
  start=$(date +%s)
  curl -fsS --max-time 600 "localhost:$LPORT/api/chat" -H 'Content-Type: application/json' \
    -d "{\"model\":\"$MODEL\",\"stream\":false,\"options\":{\"num_ctx\":16384},\"keep_alive\":\"1h\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with one word: ready\"}]}" \
    >/dev/null || die "the model didn't answer"
  echo "   answered in $(( $(date +%s) - start ))s"

  # Tell the server which model to use, and clear a tunnel left over from a dropped connection.
  ssh "${ssh_opts[@]}" "$server" "echo '$MODEL' > ~/.hody-model; echo '$WHERE' > ~/.hody-ollama-where; if command -v fuser >/dev/null; then sudo fuser -k -n tcp 11434 >/dev/null 2>&1 || true; fi"

  say "Connected. LEAVE THIS WINDOW OPEN until the test is finished."
  echo "   Keep the Mac plugged in with the lid open${GPU:+, Tailscale on, and the GPU machine awake}."
  echo "   Ctrl-C here closes everything."
  echo "   Next, in a NEW terminal window:  bash $0 ssh $key $ip"
  caffeinate -is -w $$ &
  while true; do
    if [ -n "$GPU" ] && ! ssh -S "$GPU_SOCK" -O check "$GPU" >/dev/null 2>&1; then
      warn "Lost the GPU machine. Reconnecting (is Tailscale still on?)"
      open_gpu || true
    fi
    if [ -z "$tun_pid" ] || ! kill -0 "$tun_pid" 2>/dev/null; then
      [ -n "$tun_pid" ] && warn "Tunnel to the server closed. Reopening."
      ssh "${ssh_opts[@]}" -N -o ExitOnForwardFailure=yes -R "11434:localhost:$LPORT" "$server" &
      tun_pid=$!
    fi
    sleep 10
  done
}

case $cmd in
  up) up ;;
  ssh) copy_scripts; exec ssh "${ssh_opts[@]}" -t "$server" 'tmux new -A -s hody' ;;
  fetch)
    dest="$HOME/Downloads/hody-live-$(date +%Y%m%d-%H%M)"
    mkdir -p "$dest"
    scp "${ssh_opts[@]}" "$server:~/hody-live-results.tgz" "$server:~/LIVE_EVAL.md" "$dest/" ||
      die "nothing to download yet. Has 'bash ~/ec2.sh' finished on the server?"
    say "Saved to $dest"
    if [ -d "$here/../.git" ] && [ -d "$here/../docs" ]; then
      cp "$dest/LIVE_EVAL.md" "$here/../docs/LIVE_EVAL.md"
      echo "   Also copied to docs/LIVE_EVAL.md in your repo. Next: git add docs/LIVE_EVAL.md && git commit"
    fi ;;
  *) usage ;;
esac
