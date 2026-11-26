#!/usr/bin/env python3
"""A small local helper that lets Hody use a free hosted AI API.

Hody sends its requests to this helper on 127.0.0.1. The helper forwards each
one to the provider unchanged, adding the API key, and deals with free-tier
limits so a single "slow down" reply doesn't fail a whole scenario:

  * paces requests (at least MIN_INTERVAL seconds apart),
  * on HTTP 429/5xx or a network error, waits (honouring Retry-After or the
    provider's "retry in Ns" hint) and tries again, up to MAX_WAIT seconds,
  * stops immediately with a clear message when a DAILY quota is used up,
  * if the provider rejects `response_format`, resends without it from then on
    (Hody's prompts already ask for JSON, and Hody validates every answer),
  * removes a leading <think>...</think> block from answers (thinking models
    such as qwen3 may put their reasoning there; Hody wants only the JSON),
  * counts requests and tokens in USAGE_FILE.

It never changes what is sent to the model, never logs prompts or answers (Hody already
records every request it sends), and never logs the key. Standard library only.

Settings come from the environment:
  UPSTREAM      provider base URL, e.g. https://api.mistral.ai/v1   (required)
  API_KEY       provider key (leave empty for Ollama)
  MODEL         model name, reported on /v1/models                   (required)
  PORT          default 11500
  MIN_INTERVAL  seconds between requests, default 2
  MAX_WAIT      seconds to keep retrying one request, default 480
  UPSTREAM_TIMEOUT  seconds to wait for one answer, default 600
  OLLAMA_NATIVE set to 1 when UPSTREAM is an Ollama server (e.g. http://localhost:11434):
                requests then go to Ollama's own /api/chat, which lets us set the
                context window per request, so the Ollama machine needs no settings
  NUM_CTX       context window for OLLAMA_NATIVE, default 16384
  USAGE_FILE    default ~/hody-llm-usage.json
"""
import json
import os
import re
import sys
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

UPSTREAM = os.environ.get("UPSTREAM", "").rstrip("/")
API_KEY = os.environ.get("API_KEY", "")
MODEL = os.environ.get("MODEL", "")
PORT = int(os.environ.get("PORT", "11500"))
MIN_INTERVAL = float(os.environ.get("MIN_INTERVAL", "2"))
MAX_WAIT = float(os.environ.get("MAX_WAIT", "480"))
UPSTREAM_TIMEOUT = float(os.environ.get("UPSTREAM_TIMEOUT", "600"))
OLLAMA_NATIVE = os.environ.get("OLLAMA_NATIVE", "") == "1"
NUM_CTX = int(os.environ.get("NUM_CTX", "16384"))
USAGE_FILE = os.path.expanduser(os.environ.get("USAGE_FILE", "~/hody-llm-usage.json"))
RETRY_STATUS = {429, 500, 502, 503, 504, 529}
THINK = re.compile(r"^\s*<think>.*?</think>\s*", re.S)
DAILY = re.compile(r"per[ _-]?day|perday|daily|requests per day|tokens per day|RPD|TPD", re.I)

lock = threading.Lock()  # one request to the provider at a time
state = {"last": 0.0, "drop_response_format": False}
usage = {"model": MODEL, "upstream": UPSTREAM, "requests": 0, "retries": 0, "failed": 0,
         "prompt_tokens": 0, "completion_tokens": 0, "think_blocks_removed": 0, "started": time.strftime("%Y-%m-%d %H:%M:%S")}


def log(msg):
    print(time.strftime("%H:%M:%S"), msg, flush=True)


def save_usage():
    tmp = USAGE_FILE + ".tmp"
    with open(tmp, "w") as f:
        json.dump(usage, f, indent=2)
    os.replace(tmp, USAGE_FILE)


def wait_hint(headers, body):
    """Seconds the provider asked us to wait, or None."""
    ra = headers.get("Retry-After") if headers else None
    if ra:
        try:
            return float(ra)
        except ValueError:
            pass
    m = re.search(r'(?:retry|try again)(?:Delay|[ _-]?after|[ _-]?in)?["\s:=]*"?([0-9]+(?:\.[0-9]+)?)\s*(ms|s)\b', body, re.I)
    if m:
        n = float(m.group(1))
        return n / 1000 if m.group(2).lower() == "ms" else n
    return None


def to_native(payload):
    """An OpenAI chat request as an Ollama /api/chat request."""
    options = {"num_ctx": NUM_CTX}
    if "temperature" in payload:
        options["temperature"] = payload["temperature"]
    req = {"model": payload.get("model"), "messages": payload.get("messages", []),
           "stream": False, "options": options, "keep_alive": "1h"}
    if (payload.get("response_format") or {}).get("type") == "json_object":
        req["format"] = "json"
    return req


def from_native(body):
    """An Ollama /api/chat answer as an OpenAI chat completion."""
    d = json.loads(body)
    msg = d.get("message") or {}
    return json.dumps({
        "object": "chat.completion", "model": d.get("model"),
        "choices": [{"index": 0, "finish_reason": d.get("done_reason") or "stop",
                     "message": {"role": "assistant", "content": msg.get("content", "")}}],
        "usage": {"prompt_tokens": d.get("prompt_eval_count") or 0,
                  "completion_tokens": d.get("eval_count") or 0},
    }).encode()


def send(payload):
    """POST payload upstream once. Returns (status, headers, body_bytes)."""
    if OLLAMA_NATIVE:
        url, payload = UPSTREAM + "/api/chat", to_native(payload)
    else:
        url = UPSTREAM + "/chat/completions"
    headers = {"Content-Type": "application/json", "User-Agent": "hody-llm-proxy"}
    if API_KEY:
        headers["Authorization"] = "Bearer " + API_KEY
    req = urllib.request.Request(url, data=json.dumps(payload).encode(),
                                 method="POST", headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=UPSTREAM_TIMEOUT) as r:
            body = r.read()
            if OLLAMA_NATIVE and r.status == 200:
                try:
                    body = from_native(body)
                except (ValueError, AttributeError):
                    return 502, r.headers, b'{"error":"Ollama answer was not JSON"}'
            return r.status, r.headers, body
    except urllib.error.HTTPError as e:
        return e.code, e.headers, e.read()
    except (urllib.error.URLError, TimeoutError, ConnectionError, OSError) as e:
        return 0, None, str(e).encode()


def forward(payload):
    deadline = time.time() + MAX_WAIT
    delay = 5.0
    while True:
        if state["drop_response_format"]:
            payload.pop("response_format", None)
        with lock:
            gap = state["last"] + MIN_INTERVAL - time.time()
            if gap > 0:
                time.sleep(gap)
            state["last"] = time.time()
            status, headers, body = send(payload)
        text = body.decode("utf-8", "replace")
        if status == 200:
            usage["requests"] += 1
            try:
                data = json.loads(body)
                changed = False
                for choice in data.get("choices") or []:
                    msg = choice.get("message") or {}
                    content = msg.get("content")
                    if isinstance(content, str) and THINK.match(content):
                        msg["content"] = THINK.sub("", content, count=1)
                        changed = True
                if changed:
                    usage["think_blocks_removed"] += 1
                    body = json.dumps(data).encode()
                u = data.get("usage") or {}
                usage["prompt_tokens"] += int(u.get("prompt_tokens") or 0)
                usage["completion_tokens"] += int(u.get("completion_tokens") or 0)
            except (ValueError, AttributeError, TypeError):
                pass
            save_usage()
            return status, body
        if status == 400 and "response_format" in payload and re.search(r"response_format|json_object", text, re.I):
            log("provider rejected response_format; sending without it from now on")
            state["drop_response_format"] = True
            continue
        if status == 429 and DAILY.search(text):
            log("DAILY QUOTA USED UP. Wait until it resets, or switch provider (bash ~/ec2.sh api). Provider said: " + text[:300])
            usage["failed"] += 1
            save_usage()
            return status, body
        if status in RETRY_STATUS or status == 0:
            hint = wait_hint(headers, text)
            pause = min(max(hint if hint is not None else delay, 1.0), 90.0)
            if time.time() + pause > deadline:
                log("giving up after %ds of retries (HTTP %s): %s" % (MAX_WAIT, status, text[:200]))
                usage["failed"] += 1
                save_usage()
                return (status or 502), body
            usage["retries"] += 1
            log("HTTP %s from provider, waiting %.0fs and retrying" % (status or "network error", pause))
            time.sleep(pause)
            delay = min(delay * 2, 60.0)
            continue
        log("provider returned HTTP %s: %s" % (status, text[:300]))
        usage["failed"] += 1
        save_usage()
        return status, body


class Handler(BaseHTTPRequestHandler):
    def reply(self, status, body, ctype="application/json"):
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.rstrip("/") in ("/v1/models", "/models"):
            self.reply(200, json.dumps({"object": "list", "data": [{"id": MODEL, "object": "model"}]}).encode())
        elif self.path.rstrip("/") in ("", "/health"):
            self.reply(200, b"ok\n", "text/plain")
        else:
            self.reply(404, b'{"error":"not found"}')

    def do_POST(self):
        if self.path.rstrip("/") not in ("/v1/chat/completions", "/chat/completions"):
            return self.reply(404, b'{"error":"not found"}')
        try:
            n = int(self.headers.get("Content-Length") or 0)
            payload = json.loads(self.rfile.read(n))
        except ValueError:
            return self.reply(400, b'{"error":"body is not JSON"}')
        status, body = forward(payload)
        self.reply(status, body)

    def log_message(self, *args):  # keep the log to our own one-line messages
        pass


if __name__ == "__main__":
    missing = [k for k, v in (("UPSTREAM", UPSTREAM), ("MODEL", MODEL)) if not v]
    if missing:
        sys.exit("llm-proxy: missing " + ", ".join(missing))
    save_usage()
    log("forwarding 127.0.0.1:%d -> %s%s (model %s, %.1fs between requests)"
        % (PORT, UPSTREAM, " [Ollama API, context %d]" % NUM_CTX if OLLAMA_NATIVE else "", MODEL, MIN_INTERVAL))
    ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
