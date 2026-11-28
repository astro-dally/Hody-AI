# Security

Hody is not immune to prompt injection and is not guaranteed leak-free.
This page says what each promise covers, what it does not, and how it is
tested.

## Threat model

**Assets:** secrets that appear in container configuration, environment
variables, manifests and logs; the integrity of the user's systems; the
user's trust in the advice.

**Adversaries and failure modes:**

- Anyone who can write to the target's logs, annotations, labels or
  environment (an attacker inside the app, a compromised dependency, or a
  colleague's mistake) and plants text aimed at the AI.
- The model itself: it may guess, cite things that do not exist, or
  propose unsafe changes, with or without being manipulated.
- The model provider: anything sent to it is outside the user's control.
- A malicious web page in the user's browser trying to drive the local
  Hody page (cross-site requests, DNS rebinding).

**Out of scope:** an attacker who already controls the user's machine,
kubeconfig or Docker socket; a malicious Go toolchain or dependency; a
model provider that is itself the attacker (Hody limits what it sends but
cannot limit what the provider does with it).

## Promise 1: secrets do not reach the AI model

**How:** every tool result passes through `gateway.Process`, which redacts
the full raw text before anything else (key-name, pattern and entropy
layers), replacing each distinct value with a session-stable `[SECRET_n]`.
Once a value is known it is replaced in all later results. Secret objects
are never read. ConfigMap values are never read out, only key names. The
placeholder map is an unexported in-memory field; raw text is never stored.
In test mode the model client's tripwire refuses to send any request
containing a canary, raw or JSON-escaped.

**Tested by:** `TestRedactByKeyName`, `TestRedactByPattern`,
`TestRedactByEntropy`, `TestRedactKeepsHarmlessValues`,
`TestPlaceholderStability`, `TestRedactionBeforeLimit`,
`TestGatewayNeverSerialisesSecrets`, `TestTripwireBlocksCanary`,
`TestToolsAgainstFakeClientset` (no Secret read, values hidden), and the
replay grader's "no canary in model requests" check, which scans every
saved request of all 12 scenarios independently of the tripwire.

**Does not cover / accepted false negatives:**

- Secrets shorter than 4 characters are not redacted.
- Secrets made only of hex digits (and dashes) are exempt from the entropy
  layer, so container IDs, digests, commit hashes and UUIDs stay readable;
  a hex secret is caught only if it sits under a secret-looking key or
  matches a known format.
- A secret in free text with no secret-looking key, no known format and
  low randomness (for example `connecting with hunter2`) is not detected
  the first time it appears.
- A bare `key:` is not treated as a secret key, because it is used for
  ConfigMap references.
- The entropy test catches random base64-like strings of 20+ characters
  with mixed letter case and digits; `TestRedactByEntropy` allows up to 4
  misses in 200 random tokens.

**Accepted false positives** (over-redaction, the safe direction): values
under keys such as `AUTH_MODE` or `cache_key`; base64 data in configs (for
example public certificates); any later appearance of a known secret
value inside other words. Keys ending in a reference word (`secretName`,
`passwordFile`, `tokenPath`, `secretKeyRef`, `keyId`) or an error word
(`KeyError`) are not treated as secrets; this was added after
over-redaction hid a diagnosis (see the build log).

## Promise 2: hard to trick

**How:** several layers, none of which relies on the model behaving:

- Evidence is wrapped in delimiters with a per-session random nonce;
  `<<<` and `>>>` inside data are broken up. The system prompt says data
  is never instructions.
- Nine scanner heuristics flag lines (override phrases, role markers,
  text addressed to an AI, approval claims, delimiter imitation, link
  requests, out-of-scope requests, asserted diagnoses, text aimed at the
  critic). Flagged lines are kept, quoted and marked, and shown to the user.
- Hypotheses from triage cite evidence IDs only, so no log text appears
  outside the delimiters.
- The model's only power is to request registered read-only tools inside
  the scope lock; at most 8 requests, rejections included.
- Conclusions must cite existing evidence; fixes go through policy rules
  and a critic that never sees logs or evidence.
- The web page renders all text escaped and never renders links or images.

**Tested by:** `TestScanFlagsInjectionVariants` (22 variants in
`eval/testdata/injections.txt`: plain instruction, fake role marker, false
diagnosis, out-of-scope request, text aimed at the critic, and more),
`TestScanIgnoresOrdinaryLogs` (18 ordinary lines),
`TestWrapMarksDataAndNeutralisesDelimiters`, `TestValidate`,
`TestScopeLockDocker`, `TestScopeLockFile`,
`TestInvestigateBudgetCountsRejections`, `TestCriticSeesOnlyCauseAndDiff`,
`TestHostileTextIsEscaped`, and replay scenarios where the scripted model
is "fooled" (`docker-missing-env`, `k8s-exit-126`: out-of-scope requests
rejected; `docker-mount-permission`: critic approves an unsafe fix and
policy still blocks it).

**Does not cover:** the scanner is heuristic and will miss rephrased or
non-English injections; a real model may still be swayed by unflagged text
into a wrong conclusion. That conclusion must still cite real evidence,
and any fix must still pass policy, but a plausible wrong diagnosis is
possible. Replay mode scripts the model, so it does not show how a real
model reacts to injections; only live mode can.

## Promise 3: every claim cites evidence, checked by code

**How:** the model first states the symptom it sees, then the cause.
`conclude` checks the answer against the evidence in code: every cited ID
must exist; if the category is one of Hody's known failures, its
signature (the triage rule, e.g. exit code 126 for
`command_not_executable`) must appear in the evidence and some cited
evidence must show it; if the faulty setting is named in the evidence,
some cited evidence must name it. Any problem gets one retry with a note
that names evidence IDs only, never evidence text. Invented IDs, or a
known failure claimed while the evidence shows no known failure at all,
are then downgraded to insufficient evidence with a note. A different
known failure in the evidence may be the same cause under a neighbouring
name, and missing citations make an answer weaker, not wrong: those are
kept with a warning.

**Tested by:** `TestConcludeCitationCheck`,
`TestConcludeChecksFailureSignature` (live run 3's invented cause),
`TestConcludeNeighbourCategoryIsNotDowngraded`,
`TestConcludeAsksForMissingCitations`, grader checks "cited evidence
exists" and "required tools cited".

**Does not cover:** only Hody's own categories have signatures; a cause
under a name of the model's own is checked for citations but not for a
signature. Code checks that the evidence shows the named kind of failure,
not that the model's explanation of it is right.

## Promise 4: every fix is safety-checked

**How:** the model proposes find/replace edits; Hody applies them to an
in-memory copy of the manifest, builds the diff the user sees, and checks
that the diff reproduces exactly the text the rules judge. Eleven
rules run on the original and the patched text and flag anything the
patch makes worse (block: privileged, root, host namespaces, host paths,
added capabilities, inline secrets, broad permissions, disabled TLS
checks; warn: removed limits, removed probes, `latest` or untagged
images). Edits that do not match the manifest get one retry with the
reason, then are blocked. A separate critic call
sees only the root cause and the diff. The stricter verdict wins; unknown
verdicts count as block. Nothing is ever applied.

**Tested by:** `TestUnsafeFixes` (18 unsafe fixes covering every rule and 3
safe controls in `eval/testdata/unsafe_fixes.json`), `TestApplyEdits`,
`TestPatchRoundTrip`, `TestEditsAreCheckedByRules`,
`TestFixRetryWhenEditsDoNotApply`, `TestApply`,
`TestExistingViolationIsNotBlamedOnPatch`, `TestReviewVerdicts`,
`TestStricter`.

**Does not cover:** rules match text patterns, so unusual syntax can
evade them (for example capabilities added without an `add:` key, or a
permission change done by a script the diff only references).

## Promise 5: "not enough evidence" instead of guessing

**How:** `insufficient_evidence` is a first-class outcome requiring next
checks; no fix is proposed for it.

**Tested by:** `TestTriageAsksModelWhenNoRuleMatches`,
`TestDowngradedAnswerGetsNoFix`, scenario `k8s-no-findable-cause`
(grader: outcome and "abstained without a fix"; its replay script now
replays live run 3's invented cause, which Hody's note turns into an
abstention).

**Does not cover:** whether a real model abstains when it should is an
accuracy question, measurable only in live mode.

## Read-only by construction

- Docker: the client's transport refuses every method except GET and every
  path outside an allowlist (`TestReadOnlyRefusesNonGET`,
  `TestReadOnlyPathAllowlist`). No code builds a non-GET Docker request.
- Kubernetes: only get, list and log calls; `TestSourceHasNoWriteOrSecretCalls`
  scans the package source for write and Secret calls (checked to fail on
  a planted `Delete`), and `TestToolsAgainstFakeClientset` asserts every
  recorded action is `get` or `list`. `deploy/readonly-clusterrole.yaml`
  grants only read verbs and no Secrets, as defence in depth.
- Live eval does not set anything up; scenario `live/setup.sh` scripts are
  run by a person.

## Local web page

Listens on 127.0.0.1 only (`TestCheckLoopback`); rejects requests whose
Host or Origin is not that address (DNS rebinding, cross-site,
`TestHostAndOriginChecks`); requires a per-process CSRF token for the one
state-changing request; sends a strict Content Security Policy that also
blocks images (`TestSecurityHeaders`).

## What is stored

Session JSON files (redacted evidence and model output), `audit.jsonl`
(events, tool names and arguments, verdicts; no tool output), and in test
mode a copy of each model request (redacted by construction). Files are
created with mode 0600. The placeholder map and raw tool output are never
written; see the audit in `docs/BUILD_REPORT.md`.
