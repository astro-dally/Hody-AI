# Security policy

Hody's whole point is handling failure data safely, so security reports
are very welcome.

## Reporting a vulnerability

Please **do not open a public issue**. Use GitHub's private reporting:
the repository's **Security** tab → **Report a vulnerability**. Include
what you ran, what happened, and what you expected; strip real secrets
from any logs you attach.

This is a personal project: reports are answered on a best-effort basis,
usually within a week.

## What counts

Anything that breaks one of Hody's promises, for example:

- a secret, token or password reaching the model (or the saved requests)
  unredacted;
- text in logs or configuration that makes Hody call a tool outside the
  diagnosed target, or follow instructions it should treat as data;
- any write to Docker or Kubernetes, or any read of Kubernetes Secrets;
- a fix that should be blocked passing the review;
- the web page being reachable from another machine or open to
  cross-site requests.

The threat model, and the limits Hody already documents (heuristic
redaction and injection scanning, text-based policy rules), are in
[docs/SECURITY.md](../docs/SECURITY.md).

## Supported versions

Only the latest release gets fixes.
