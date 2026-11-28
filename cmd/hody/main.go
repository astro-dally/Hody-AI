// Command hody diagnoses failed Docker and Kubernetes deployments without
// letting secrets reach the AI model. It only reads; it never changes a
// container, cluster or image, and it never applies a fix.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

const usage = `Hody finds out why a Docker container or Kubernetes pod failed, without
sending secrets to the AI model. It only reads and never applies fixes.

Usage:
  hody <command> [options]

Commands:
  diagnose docker <container>       find out why a Docker container failed
  diagnose k8s pod/<name> -n <ns>   find out why a Kubernetes pod failed
  serve                             open the local web page
  doctor                            check that Docker, Kubernetes and the AI model are reachable
  eval <scenario-dir>               run the evaluation scenarios
  version                           print the version

Examples:
  hody doctor
  hody diagnose docker web
  hody diagnose k8s pod/api-7d9f -n shop --file deploy/api.yaml
  hody serve --addr 127.0.0.1:8080

Run "hody <command> --help" for the options of one command.

Model settings (environment):
  HODY_LLM_BASE_URL   e.g. http://localhost:11434/v1 for Ollama
  HODY_LLM_API_KEY    optional for local models
  HODY_LLM_MODEL      e.g. qwen3:32b
  HODY_LLM_TIMEOUT    seconds one model request may take (default 180)
  HODY_DATA_DIR       where sessions and the audit log go (default ~/.hody)
  HODY_CANARIES       test mode: comma-separated canary strings
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		// A person at a terminal gets a greeting; a script gets the usage
		// on stderr and a non-zero exit, as before.
		if isTerminal(stdout) {
			fmt.Fprint(stdout, banner(useColor(stdout))+"\n"+usage)
			return 0
		}
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
	case "doctor":
		err = cmdDoctor(args[1:], stdout, realChecks())
	case "version", "--version":
		fmt.Fprintln(stdout, versionLine())
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		err = fmt.Errorf("unknown command %q (run \"hody help\")", args[0])
	}
	switch {
	case errors.Is(err, flag.ErrHelp):
		return 0 // the command printed its own help
	case errors.Is(err, errChecksFailed):
		return 1 // doctor already said what is wrong
	case err != nil:
		fmt.Fprintln(stderr, "hody:", err)
		return 1
	}
	return 0
}

// commandUsage sets a flag set's help to a synopsis, a description and the
// options.
func commandUsage(fs *flag.FlagSet, synopsis, about string) {
	fs.Usage = func() {
		w := fs.Output()
		fmt.Fprintf(w, "Usage:\n  %s\n\n%s\n", synopsis, about)
		if hasFlags(fs) {
			fmt.Fprintln(w, "\nOptions:")
			fs.PrintDefaults()
		}
	}
}

func hasFlags(fs *flag.FlagSet) bool {
	n := 0
	fs.VisitAll(func(*flag.Flag) { n++ })
	return n > 0
}
