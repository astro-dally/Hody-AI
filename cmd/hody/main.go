// Command hody diagnoses failed Docker and Kubernetes deployments without
// letting secrets reach the AI model. It only reads; it never changes a
// container, cluster or image, and it never applies a fix.
package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `Hody finds why a Docker container or Kubernetes pod failed, without
sending secrets to the AI model. It only reads and never applies fixes.

Usage:
  hody serve [--addr 127.0.0.1:8080]
  hody diagnose docker <container> [--file path] [--socket path] [--json]
  hody diagnose k8s pod/<name> -n <namespace> [--kubeconfig path] [--file path] [--json]
  hody eval <scenario-dir> [--live] [--work dir]

Model settings (environment):
  HODY_LLM_BASE_URL   e.g. http://localhost:11434/v1 for Ollama
  HODY_LLM_API_KEY    optional for local models
  HODY_LLM_MODEL      e.g. llama3.1
  HODY_LLM_TIMEOUT    seconds one model request may take (default 180)
  HODY_DATA_DIR       where sessions and the audit log go (default ~/.hody)
  HODY_CANARIES       test mode: comma-separated canary strings
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "serve":
		err = cmdServe(args[1:], stderr)
	case "diagnose":
		err = cmdDiagnose(args[1:], stdout)
	case "eval":
		err = cmdEval(args[1:], stdout)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		fmt.Fprintln(stderr, "hody:", err)
		return 1
	}
	return 0
}
