# Hody live test: the complete guide

This guide takes you from nothing to a finished, recorded live evaluation of Hody, saved in your GitHub repo. Follow the parts in order. Every command is in a grey box: copy it, paste it into the right terminal, press Enter.

Three small scripts do most of the work:

| Script | Runs on | What it does |
|---|---|---|
| `scripts/mac.sh` | your Mac | Connects the server to the AI model on your GPU machine, logs you into the server, downloads the results |
| `scripts/ec2.sh` | the EC2 server | Installs the tools, downloads Hody from GitHub, builds it, runs every check, runs the live test, packs the results |
| `scripts/llm-proxy.py` | the EC2 server | A small helper between Hody and the model. It retries when a connection blips or a provider says "slow down", asks Ollama for a big enough context window, and removes qwen3's `<think>` notes from answers. It never changes what Hody sends |

---

## 0. The big picture

```
 GPU machine                 Your Mac (the bridge)              EC2 server (Ubuntu, in AWS)
┌──────────────────┐  Tailscale  ┌────────────────────┐  SSH tunnel  ┌──────────────────────────┐
│ Ollama           │◄────SSH─────│ Terminal 1:        │◄─────────────│ Hody ─► llm-proxy.py     │
│ qwen3:30b on GPU │             │   mac.sh up        │              │   reads ─► Docker        │
└──────────────────┘             │ Terminal 2:        │              │          ─► kind cluster │
                                 │   mac.sh ssh ──────┼─────────────►│ ec2.sh (inside tmux)     │
                                 └────────────────────┘              └──────────────────────────┘
```

- The **server** creates real broken containers and pods, and runs Hody against them.
- The **GPU machine** runs the AI model (`qwen3:30b`). Nothing on it needs changing.
- The **Mac** is the bridge. The server can't see your Tailscale network, but it can reach the Mac, and the Mac can reach the GPU machine. The Mac doesn't run any model, so it needs no extra disk space.

**Time:** about 1 hour of setup, then 30–60 minutes for the test to run by itself.
**Cost:** about $0.17 per hour while the server is on (`t3.xlarge`), so well under $5 in total.

No GPU machine available? Part J (Plan B) uses a free hosted AI API instead.

### Words you'll see

| Word | Meaning |
|---|---|
| Terminal | The app where you type commands. On the Mac: press `Cmd + Space`, type `Terminal`, press Enter |
| `~` | Your home folder. On the Mac it's `/Users/<your-name>`; on the server it's `/home/ubuntu` |
| SSH | A secure way to type commands on another computer |
| `.pem` key | The file that proves to the server that it's you. Treat it like a password |
| Tailscale | A private network between your own devices; it lets the Mac reach the GPU machine from anywhere |
| tmux | Keeps programs running on the server even if your connection drops |
| `<PUBLIC-IP>` | Replace this (including the `< >`) with your server's IP address, e.g. `3.110.45.12` |
| `<GPU>` | Replace this with how you SSH into your GPU machine, e.g. `dally@gpu-box` |

---

## 1. Before you start: checklist

- [ ] An AWS account with your credits added (**Billing → Credits** shows them)
- [ ] Your MacBook Pro M2, plugged into power
- [ ] Your GPU machine switched on, with Ollama and `qwen3:30b` installed, and Tailscale running on it and on the Mac
- [ ] The updated repository from Claude, `Hody-AI-updated.zip`, in `~/Downloads`. It is your GitHub repository with the live-test work added as new commits, and it already contains the three scripts and this guide

---

## Part A. Put the updated code on GitHub

The server downloads Hody from GitHub, so GitHub must have the updated code first. You do this part **on your Mac**, once.

### A1. Unpack the updated repository

If you already have a folder called `~/Hody-AI` (your old clone), move it out of the way first. Nothing in it is lost:

```sh
[ -d ~/Hody-AI ] && mv ~/Hody-AI ~/Hody-AI-old
```

Then unpack the zip into your home folder:

```sh
cd ~
unzip -q ~/Downloads/Hody-AI-updated.zip
cd ~/Hody-AI
```

### A2. Check what's in it

```sh
git status
git log --oneline
```

`git status` should say `nothing to commit, working tree clean`, and `git log` should show three commits, newest first:

```
xxxxxxx scripts: one-command live test on EC2 (mac.sh, ec2.sh, llm-proxy.py) and guide
56fd710 eval: make live mode runnable and add the live runner
dc0e99e feat: initial commit — Hody AI container failure diagnosis
```

The bottom one is the commit already on GitHub; the two above it are new. The folder is a normal git clone: it remembers your GitHub repository as `origin`, so you can pull and push as usual.

### A3. Push to GitHub

```sh
git push
```

If `git push` asks for a password, GitHub wants a **Personal Access Token**, not your account password. Make one at **GitHub → Settings → Developer settings → Personal access tokens → Tokens (classic) → Generate new token**, tick `repo`, and paste the token as the password.

Check it worked: open https://github.com/astro-dally/Hody-AI. You should see "3 commits", the `scripts/` folder, and `eval/run-live.sh`.

From now on, `~/Hody-AI` is your working copy. Once you're happy, you can delete `~/Hody-AI-old`.

---

## Part B. Set up AWS

All of this is in the AWS web console (https://console.aws.amazon.com). If your server is already running, skip to B3 and check its settings match.

### B1. Safety net: a budget alert

1. Search **Budgets** in the top search bar → **Create budget**.
2. Choose **Use a template → Monthly cost budget**.
3. Amount: `10` (dollars). Enter your email. **Create budget**.

AWS emails you if spending heads past $10, so a forgotten server can't eat your credits.

### B2. Pick a region

Top-right corner of the console. Any region works; one near you (e.g. **Asia Pacific (Mumbai)**) makes typing on the server feel snappier. Stay in the same region every time, or you won't see your server.

### B3. Launch the server

**EC2 → Instances → Launch instances**, then fill in:

| Setting | Value |
|---|---|
| Name | `hody-test` |
| Application and OS Images | **Ubuntu**, then **Ubuntu Server 24.04 LTS**, architecture **64-bit (x86)** |
| Instance type | **`t3.xlarge`** (4 vCPU, 16 GiB) |
| Key pair | **Create new key pair** → name `hody-key`, type **RSA**, format **.pem** → it downloads to your Mac |
| Network settings | Tick **Allow SSH traffic from** and choose **My IP** (not "Anywhere") |
| Configure storage | **30** GiB, **gp3** |

Click **Launch instance**, then **View all instances**. Wait until **Instance state** says *Running* and **Status check** says *2/2 checks passed* (about 2 minutes).

Click the instance and copy its **Public IPv4 address**. That's your `<PUBLIC-IP>`.

### B4. Put the key somewhere safe

On your Mac (Terminal):

```sh
mkdir -p ~/.ssh
mv ~/Downloads/hody-key.pem ~/.ssh/
chmod 400 ~/.ssh/hody-key.pem
```

`chmod 400` makes the key readable only by you; SSH refuses to use a key that others could read.

---

## Part C. Check the GPU machine (2 minutes)

You don't change anything on the GPU machine. You only check the Mac can reach it.

**1. Tailscale is on** on both the Mac and the GPU machine (the Tailscale icon in the Mac's menu bar shows *Connected*).

**2. You can SSH in from the Mac.** Use the same name you normally use; that's your `<GPU>`:

```sh
ssh <GPU>
```

**3. Ollama is running and has the model.** While you're logged in to the GPU machine:

```sh
ollama list
```

You should see `qwen3:30b` in the list. If `ollama list` says it can't connect, start Ollama (`ollama serve`, or open the Ollama app). Then type `exit` to come back to the Mac.

**4. (Recommended) Log in with a key instead of a password.** If step 2 asked for a password, run this once:

```sh
ssh-copy-id <GPU>
```

It will ask for the password one last time. Afterwards `mac.sh` can reconnect by itself if the connection drops. Without it, `mac.sh` asks for the password at the start and again after every reconnect.

**5. Stop the GPU machine from sleeping** during the test. Check its power settings, or keep it busy and plugged in.

There's no need to set Ollama's context window on the GPU machine. The helper asks for 16,384 tokens with every request.

---

## Part D. Terminal 1 (Mac): connect everything

Open Terminal and run (replace `<GPU>` and `<PUBLIC-IP>`):

```sh
cd ~/Hody-AI
GPU=<GPU> bash scripts/mac.sh up ~/.ssh/hody-key.pem <PUBLIC-IP>
```

What happens, in order:

1. Copies `ec2.sh` and `llm-proxy.py` to the server.
2. Connects to the GPU machine over Tailscale and checks that Ollama has `qwen3:30b`.
3. Asks the model a test question and shows how long it took. The first answer can take a minute while the model loads.
4. Tells the server which model to use.
5. Opens the tunnel to the server and prints `Connected. LEAVE THIS WINDOW OPEN…`

After that it looks like nothing is happening. **That's correct: the connections are working.** Leave this window open for the whole test.

- It keeps your Mac awake. Keep the Mac **plugged in with the lid open**, and Tailscale on.
- It checks both connections every 10 seconds and reopens whichever one dropped.
- `Ctrl + C` in this window closes everything (do that at the very end).
- To use another model on the GPU machine, put `MODEL=<name>` in front, e.g. `MODEL=qwen3:30b GPU=<GPU> bash scripts/mac.sh up …`.

---

## Part E. Terminal 2 (Mac): log into the server

Open a **new** Terminal window (`Cmd + N`) and run:

```sh
cd ~/Hody-AI
bash scripts/mac.sh ssh ~/.ssh/hody-key.pem <PUBLIC-IP>
```

Your prompt changes to something like `ubuntu@ip-172-31-5-20:~$`, and a green bar appears at the bottom. **You're now typing on the server, inside tmux.**

tmux in 30 seconds:

| You want to | Do this |
|---|---|
| Leave the server but keep the test running | Press `Ctrl + b`, let go, then press `d` |
| Come back to it | Run the same `bash scripts/mac.sh ssh …` command again |
| Scroll up in tmux | `Ctrl + b`, then `[`, then the arrow keys; press `q` to stop scrolling |

Everything in Part F is typed **in this window** (on the server).

---

## Part F. On the server: install, test, run

The scripts are already in your home folder on the server (Part D copied them). You run `ec2.sh` three times, each going a bit further. It's safe to run again: anything already done is skipped.

### F1. Install the tools (about 5 minutes)

```sh
bash ~/ec2.sh setup
```

Installs Docker, Go, kind, kubectl and a few helpers. It ends with `Tools are ready.`

Then type `exit` and press Enter. This closes the session so your next login gets the new Docker permissions. Reconnect with the Part E command.

### F2. Checks and smoke test (about 10 minutes)

```sh
bash ~/ec2.sh smoke
```

What it does, in order:

1. **Checks the model.** It starts the helper and asks `qwen3:30b` a test question in JSON mode. It should print something like `answered in 12s: {"status": "ready"}`.
2. **Downloads Hody** from your GitHub repo into `~/Hody-AI` (or updates it with `git pull`). It stops if `eval/run-live.sh` is missing, which means Part A isn't pushed yet.
3. **Offline checks.** It verifies dependencies, builds Hody, runs formatting/vet/all unit tests, the replay eval (should be 12 of 12), and a rehearsal of the live runner.
4. **Prepares the test cluster.** It downloads the container images once, creates the kind Kubernetes cluster, and loads the images into it.
5. **Smoke test.** It runs one real scenario (`docker-oom`) end to end and shows its summary.

It ends with `Smoke test passed. Next: bash ~/ec2.sh`.

### F3. The full live test (30–60 minutes)

```sh
bash ~/ec2.sh
```

It repeats the quick checks, then runs all 12 scenarios one at a time: create the failure, wait for it to fail, run Hody with the real model, clean up. You can leave while it runs (`Ctrl + b`, then `d`), but keep Terminal 1 open, the Mac awake, and the GPU machine on.

At the end it prints the summary and saves two files on the server:

| File | What it is |
|---|---|
| `~/LIVE_EVAL.md` | The readable summary, plus a short "How the model was reached" note with request and token counts |
| `~/hody-live-results.tgz` | Everything: summary, grades, logs, every model request, every diagnosis, the helper's log |

Every run also writes a log file, `~/hody-<date-time>.log`, and the helper writes `~/hody-llm-proxy.log`. If anything fails, those are what to show Claude.

---

## Part G. Bring the results home and save them in the repo

Open a **third** Terminal window on your Mac:

```sh
cd ~/Hody-AI
bash scripts/mac.sh fetch ~/.ssh/hody-key.pem <PUBLIC-IP>
```

This downloads both files into `~/hody-results-<date-time>/` (not `~/Downloads`, which macOS keeps Terminal from reading) and makes `~/hody-results-<date-time>.zip` next to it. Share that zip with Claude, who records the run in `docs/LIVE_EVAL.md`, updates `README.md` and `docs/BUILD_REPORT.md`, and sends the change as a patch. The script leaves your repo untouched, so the patch applies cleanly.

The big `.tgz` file is for you to keep, not for git: the repo deliberately ignores `eval/results/`.

---

## Part H. Read the results

The top of `LIVE_EVAL.md` looks like this (numbers here are only an example):

```
**10 of 12 scenarios passed all checks.**

Accuracy over the 11 scenarios with a known cause (…): category 9/11, object 11/11, field 8/11.
Abstention (k8s-no-findable-cause, …): correct (outcome `insufficient_evidence`).
Safety: no planted secret appeared in any of the 97 requests sent to the model. Injection lines flagged in 12 of 12 graded scenarios.

| Scenario   | Outcome    | Verdict | Checks | Category | Object | Field | Result | Time  |
| docker-oom | root_cause | approve | 7/7    | yes      | yes    | yes   | PASS   | 2m10s |
```

| Column | Meaning |
|---|---|
| Outcome | `root_cause` = found a cause; `insufficient_evidence` = said "I don't know" |
| Verdict | The fix review: `approve` (safe), `warn`, or `block` (unsafe, so Hody stopped it). Shown, not graded |
| Checks | How many of the grader's checks passed |
| Category / Object / Field | Did the model's answer match the expected answer? |
| Result | `PASS` only if every check passed |

How to judge it:

- **"no canary in model requests" must pass everywhere.** A failure there means a secret reached the model. That's a real bug; tell Claude immediately.
- **Accuracy misses are usually the model, not Hody.** Comparing two models is the fair test.
- Two known quirks: `k8s-exit-126` expects the field `command`, so a correct answer about `defaultMode` counts as a miss; and the category must be spelled exactly (e.g. `oom_killed`).

The summary's own "How to read this" section, and `eval/README.md`, explain every check in more detail.

---

## Part I. Shut down and keep costs at zero

When you're done for the day:

1. On the server (Terminal 2): `kind delete cluster`
2. On the Mac, Terminal 1: press `Ctrl + C` to close the connections.
3. In the AWS console: select the instance → **Instance state → Stop instance**.

A stopped server costs nothing for computing. Only its 30 GB disk is charged (about $2–3 a month).

When the project is completely finished and the results are on GitHub: **Instance state → Terminate instance**. That deletes the server and its disk, so the charges stop completely.

### Coming back after stopping

1. **Instance state → Start instance**. The **public IP changes**, so copy the new one.
2. Run Part D and Part E again with the new IP.
3. On the server, re-run whichever step you need. For example, `bash ~/ec2.sh` for another full run.

---

## Part J. Plan B: a free hosted AI API (no GPU machine needed)

Use this if the GPU machine isn't available, or to compare a second model. Hody works with any OpenAI-compatible API, and the helper waits and retries when a free plan says "slow down".

**1. Get a free key** from one of these:

| Provider | Where to get a key | Notes |
|---|---|---|
| Google Gemini | https://aistudio.google.com → *Get API key* | Strong model, big context window. Check the free plan's requests-per-day limit is at least ~200 |
| Mistral | https://console.mistral.ai → choose the free *Experiment* plan → *API Keys* | Generous token allowance; asks for phone verification |
| Groq | https://console.groq.com → *API Keys* | Very fast, but its free plan caps tokens per minute and per day, which a full run can hit |

Free plans change often, so check the provider's current limits page. A full run makes roughly 60–150 requests of a few thousand tokens each. Free plans may use what you send to improve their models. That's fine here, because every scenario uses fake data and fake secrets, but **never point a free API at real company logs**.

**2. On the server**, run this and answer the three questions: provider, model name (press Enter for the suggested one), and the key. The key stays hidden while you type:

```sh
bash ~/ec2.sh api
```

The key is saved in `~/.hody-llm`, which only you can read, and the command tests it straight away. Hody itself never sees the key: only the helper does.

**3. Continue with F2 and F3** as usual (`bash ~/ec2.sh smoke`, then `bash ~/ec2.sh`). You only need Terminal 1 (`mac.sh up`) when using Ollama, so with an API the Mac is free to sleep once the test is running.

To go back to the GPU machine: `rm ~/.hody-llm` on the server. When the project is done, delete the key on the provider's website.

---

## Troubleshooting

When a script stops, it prints a red `STOP:` line saying what went wrong. The common ones:

| You see | What to do |
|---|---|
| `can't reach ubuntu@…` / `can't SSH to ubuntu@…` | Is the instance *Running*? Did the IP change after a stop/start? Did your own IP change (new Wi-Fi, hotspot)? Fix the last one in **EC2 → Security Groups → Inbound rules → Edit → Source: My IP** |
| `can't SSH to <GPU>` | Is Tailscale *Connected* on both machines? Is the GPU machine awake? Does `ssh <GPU>` work by itself? |
| `Ollama isn't answering on the GPU machine` | Log in with `ssh <GPU>` and start it: `ollama serve` (or open the Ollama app) |
| `model qwen3:30b is not on the GPU machine` | It lists the models it found; use one with `MODEL=<name>`, or `ollama pull qwen3:30b` on the GPU machine |
| `Lost the GPU machine. Reconnecting` keeps repeating | Tailscale dropped or the GPU machine went to sleep. Fix that; the script reconnects by itself |
| `WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED` | AWS reused an IP. Run `ssh-keygen -R <PUBLIC-IP>`, then try again |
| `tmux: command not found` (from `mac.sh ssh`) | Log in plainly with `ssh -i ~/.ssh/hody-key.pem ubuntu@<PUBLIC-IP>` and run `bash ~/ec2.sh setup` |
| `can't reach Ollama` (on the server) | Is Terminal 1 still open and connected? Is the Mac awake? Restart `mac.sh up` |
| `The answer doesn't start with '{'` | The model answered in a form Hody can't read. Send Claude the line it printed |
| `DAILY QUOTA USED UP` in `~/hody-llm-proxy.log` | The free API plan's daily limit ran out. Wait until it resets, or switch provider with `bash ~/ec2.sh api` |
| `this version of Hody has no eval/run-live.sh` | Part A isn't on GitHub yet. Finish A3, then re-run; it will `git pull` |
| `git clone failed` | The repo is private. Make it public on GitHub (**Settings → Danger Zone**) or ask Claude |
| `the step above failed. Full log: …` | Send Claude the last 30 lines: `tail -30 ~/hody-*.log ~/hody-llm-proxy.log` |
| `could not pull … toomanyrequests` | Docker Hub's download limit. Make a free Docker Hub account, run `docker login`, re-run |
| `permission denied … docker.sock` when typing docker yourself | Type `exit`, reconnect with Part E |
| Each model answer takes many minutes | Check the GPU machine isn't busy with something else. `tail -f ~/hody-llm-proxy.log` on the server shows each retry |
| You want to look at a failed scenario yourself | `cd ~/Hody-AI && KEEP=1 bash eval/run-live.sh <scenario-name>` leaves it running; clean up with its `live/cleanup.sh` |

---

## Cheat sheet

```sh
# ── Mac, Terminal 1 (keep open) ──────────────────────────────
cd ~/Hody-AI && GPU=<GPU> bash scripts/mac.sh up ~/.ssh/hody-key.pem <PUBLIC-IP>

# ── Mac, Terminal 2 → you're on the server, inside tmux ──────
cd ~/Hody-AI && bash scripts/mac.sh ssh ~/.ssh/hody-key.pem <PUBLIC-IP>

# ── Server (Terminal 2) ─────────────────────────────────────
bash ~/ec2.sh setup     # first time only, then: exit, and reconnect
bash ~/ec2.sh smoke     # checks + one real scenario
bash ~/ec2.sh           # the full live test
bash ~/ec2.sh api       # Plan B only: use a free hosted API instead
kind delete cluster     # when finished

# ── Mac, Terminal 3 ─────────────────────────────────────────
cd ~/Hody-AI && bash scripts/mac.sh fetch ~/.ssh/hody-key.pem <PUBLIC-IP>   # then share ~/hody-results-<date-time>.zip

# ── AWS console ─────────────────────────────────────────────
# Instance state → Stop (end of day) / Terminate (project finished)
```
