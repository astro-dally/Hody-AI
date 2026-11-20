# Usage

## Before you start

Build Hody (`go build -o hody ./cmd/hody`) and point it at a model:

```sh
export HODY_LLM_BASE_URL=http://localhost:11434/v1
export HODY_LLM_MODEL=llama3.1
export HODY_LLM_API_KEY=          # empty for a local model
```

### A local model with Ollama

1. Install Ollama from https://ollama.com and start it (`ollama serve`).
2. Pull a model that follows instructions well: `ollama pull llama3.1`
   (or `qwen2.5:14b` if your machine can run it; larger models diagnose better).
3. Ollama serves an OpenAI-compatible API at `http://localhost:11434/v1`;
   use the settings above.

Hody asks for JSON answers (`response_format: json_object`). If a small
model returns invalid JSON twice in a row, the diagnosis stops with a clear
error; try a larger model.

## Walkthrough 1: a Docker container

Start a container that runs out of memory:

```sh
docker run -d --name report-worker -m 64m --memory-swap 64m -e DB_PASSWORD=hunter2hunter2 \
  python:3.12-slim python -c "x = bytearray(512*1024*1024)"
```

Diagnose it:

```sh
./hody diagnose docker report-worker
```

Hody reads the container (inspect, logs, engine events and its
configuration), hides `DB_PASSWORD`, matches the out-of-memory signature,
asks the model to conclude and propose a fix, and reviews the fix.

The report below is **real output, but from replay mode**
(`eval/scenarios/docker-oom`, recorded tool output and a scripted model),
because the build environment had no Docker daemon or model. With a real
model the wording will differ.

```
Hody diagnosis 2fd85b4a28f25cd1 for docker report-worker: complete

Root cause (oom_killed): The worker process needs more memory than the 64 MiB limit while building the report, so the kernel kills it (OOMKilled, exit code 137).
Faulty object: container report-worker   field: mem_limit
Next check: Re-run the container after the fix and watch its logs.

Cited evidence:
--- E1 from docker.inspect_container map[container:report-worker] ---
  container: report-worker (id 8f3a2b1c9d0e)
  state: status=exited exitCode=137 oomKilled=true restartCount=4
  startedAt=2026-11-17T09:14:02Z finishedAt=2026-11-17T09:14:09Z
  image: shop/report-worker:2.3.1
  user: "" workingDir: "/app"
  entrypoint: [] cmd: ["python","worker.py"]
  env: PATH=/usr/local/bin:/usr/bin
  env: DB_HOST=db
  env: DB_PASSWORD=[SECRET_1]
  exposedPorts: []
  hostConfig: privileged=false networkMode=bridge pidMode="" capAdd=null memoryBytes=67108864 readonlyRootfs=false restartPolicy=on-failure
--- E3 from docker.events map[container:report-worker] ---
  2026-11-17T09:14:02Z container start image=shop/report-worker:2.3.1 name=report-worker
  2026-11-17T09:14:09Z container oom image=shop/report-worker:2.3.1 name=report-worker
  2026-11-17T09:14:09Z container die exitCode=137 image=shop/report-worker:2.3.1 name=report-worker

Proposed fix (not applied) against E4:
--- a/docker-compose.yml
+++ b/docker-compose.yml
@@ -10,1 +10,1 @@
-    mem_limit: 67108864
+    mem_limit: 536870912

Why: Raise the memory limit from 64 MiB to 512 MiB so the report fits. Keep a limit so one container cannot starve the host.

Review verdict: APPROVE (policy: approve, critic: approve)
Critic: Raising the memory limit addresses the OOM kill and keeps a limit in place.

1 log line(s) looked like instructions aimed at an AI; they were treated as data only.
```

Useful options: `--file docker-compose.yml` to get the fix against your own
compose file, `--json` to print the whole session, `--socket` if your
Docker socket is not `/var/run/docker.sock`.

## Walkthrough 2: a Kubernetes pod

Create a cluster with kind and a pod whose ConfigMap key does not match:

```sh
kind create cluster
eval/scenarios/k8s-configmap-key-mismatch/live/setup.sh
kubectl -n shop get pods            # note the pod name, e.g. orders-5c9d8f7b6-m4n8k
./hody diagnose k8s pod/orders-5c9d8f7b6-m4n8k -n shop
```

Hody uses your kubeconfig (`--kubeconfig` to choose another). It needs only
the permissions in `deploy/readonly-clusterrole.yaml`.

Real output, again **from replay mode**
(`eval/scenarios/k8s-configmap-key-mismatch`). Note that the model asked
for one extra tool (`k8s.configmap_keys`, evidence E7) to link the two
pieces of evidence:

```
Hody diagnosis 4ab4551e59805695 for k8s pod/orders-5c9d8f7b6-m4n8k -n shop: complete

Root cause (config_reference): The container's DATABASE_URL reads key database_url from ConfigMap orders-config, but the ConfigMap only has DATABASE_URL (upper case), so the container cannot be created.
Faulty object: deployment/orders   field: spec.template.spec.containers[orders].env[DATABASE_URL].valueFrom.configMapKeyRef.key
Next check: Apply the change with your usual deployment process and watch the rollout.

Cited evidence:
--- E1 from k8s.pod_status map[namespace:shop pod:orders-5c9d8f7b6-m4n8k] ---
  pod: shop/orders-5c9d8f7b6-m4n8k phase=Pending node=kind-worker qosClass=Burstable
  owner: ReplicaSet/orders-5c9d8f7b6
  container orders: image=shop/orders:1.12.0 ready=false restartCount=0
    state: waiting reason=CreateContainerConfigError message="couldn't find key database_url in ConfigMap shop/orders-config"
    command=[] args=[]
    resources: requests={} limits={}
    env: DATABASE_URL from configMapKeyRef orders-config key=database_url
    env: SMTP_PASSWORD=[SECRET_1]
--- E7 from k8s.configmap_keys map[name:orders-config namespace:shop] ---
  configmap shop/orders-config has 2 keys:
  key DATABASE_URL (value hidden, 48 bytes)
  key LOG_LEVEL (value hidden, 4 bytes)

Proposed fix (not applied) against E6:
--- a/manifest.yaml
+++ b/manifest.yaml
@@ -25,2 +25,2 @@
             configMapKeyRef:
-              key: database_url
+              key: DATABASE_URL

Why: Reference the key exactly as it is spelled in the ConfigMap: DATABASE_URL.

Review verdict: APPROVE (policy: approve, critic: approve)
Critic: Fixes a key reference; no security impact.

2 log line(s) looked like instructions aimed at an AI; they were treated as data only.
```

Clean up with `eval/scenarios/k8s-configmap-key-mismatch/live/cleanup.sh`
and `kind delete cluster`.

## The web page

```sh
./hody serve
```

Open http://127.0.0.1:8080, choose Docker or Kubernetes, enter the name
(and namespace), and start. The page refreshes until the diagnosis is done,
then shows the cause, the cited evidence, the diff, the review verdict with
explanations, any warnings and every tool call, including rejected ones.

## Running the evaluation

Replay (no Docker, cluster or model needed):

```sh
./hody eval eval/scenarios
```

Live (real systems and a real model). This is the only mode that measures
diagnostic accuracy:

1. Configure the model as above.
2. For Docker scenarios, have Docker running. For Kubernetes scenarios,
   `kind create cluster`.
3. Set up the scenarios you want to run. Each one has `live/setup.sh`
   (run it yourself; Hody never creates anything), for example
   `for s in eval/scenarios/*/live/setup.sh; do sh "$s"; done`.
   Kubernetes scenarios need a minute to reach their failing state.
   `k8s-unschedulable` needs nodes smaller than 64Gi of memory, which kind
   nodes are.
4. Run `./hody eval eval/scenarios --live`.
5. Clean up with each scenario's `live/cleanup.sh`.

The table's accuracy column shows whether the model's category, object and
field match the label. Live setups differ from the recorded data in small
ways (public images such as `python:3.12-slim` stand in for the fictional
`shop/...` images), so compare categories more than exact names.
