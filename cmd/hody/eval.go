package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"hody/eval"
	"hody/internal/model"
	"hody/internal/tools"
	"hody/internal/tools/docker"
)

// cmdEval runs every scenario under a directory and prints a results table.
func cmdEval(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	commandUsage(fs, "hody eval <scenario-dir> [options]",
		"Runs the evaluation scenarios. Replay mode (the default) uses recorded tool output\nand a scripted model and checks plumbing and safety; --live diagnoses real broken\ncontainers and pods with the configured model (see eval/run-live.sh).")
	live := fs.Bool("live", false, "use the real Docker daemon or cluster and the configured model (run each scenario's live/setup.sh first)")
	work := fs.String("work", "", "directory for sessions and saved model requests (default: a new temp dir)")
	var o diagnoseOpts
	fs.StringVar(&o.socket, "socket", defaultSocket(), "Docker socket path (live mode)")
	fs.StringVar(&o.kubeconfig, "kubeconfig", "", "kubeconfig path (live mode)")
	pos, err := parseInterleaved(fs, args)
	if err != nil || len(pos) != 1 {
		return errors.New("usage: hody eval <scenario-dir> [--live] [--work dir]")
	}
	if *work == "" {
		if *work, err = os.MkdirTemp("", "hody-eval-"); err != nil {
			return err
		}
	}
	scs, err := eval.LoadAll(pos[0])
	if err != nil {
		return err
	}
	factory := func(t model.Target) ([]tools.Tool, error) {
		if t.Platform == model.PlatformDocker {
			return docker.Tools(docker.New(o.socket)), nil
		}
		return k8sTools(o.kubeconfig)
	}
	ctx := context.Background()
	var grades []eval.Grade
	for _, sc := range scs {
		var r eval.Result
		if *live {
			r = eval.RunLive(ctx, sc, *work, llmConfig(""), factory, findPod(o.kubeconfig))
		} else {
			r = eval.RunReplay(ctx, sc, *work)
		}
		grades = append(grades, eval.GradeResult(r, *live))
	}
	eval.WriteTable(stdout, grades)
	data, _ := json.MarshalIndent(grades, "", "  ")
	if err := os.WriteFile(filepath.Join(*work, "results.json"), data, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Sessions, saved model requests and results.json are in %s\n", *work)
	for _, g := range grades {
		if !g.Pass {
			return errors.New("some scenarios failed")
		}
	}
	return nil
}
