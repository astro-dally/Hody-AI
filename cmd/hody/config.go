package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hody/internal/llm"
)

// dataDir is where sessions and the audit log are stored.
func dataDir() string {
	if d := os.Getenv("HODY_DATA_DIR"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".hody")
	}
	return ".hody"
}

// llmConfig reads the model settings. Canaries switch on test mode, in
// which every outgoing request is also saved under recordDir.
func llmConfig(recordDir string) llm.Config {
	cfg := llm.Config{BaseURL: os.Getenv("HODY_LLM_BASE_URL"), APIKey: os.Getenv("HODY_LLM_API_KEY"), Model: os.Getenv("HODY_LLM_MODEL")}
	for _, c := range strings.Split(os.Getenv("HODY_CANARIES"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			cfg.Canaries = append(cfg.Canaries, c)
		}
	}
	if len(cfg.Canaries) > 0 {
		cfg.RecordDir = recordDir
	}
	cfg.Timeout = llmTimeout(os.Getenv("HODY_LLM_TIMEOUT"))
	return cfg
}

// llmTimeout parses HODY_LLM_TIMEOUT, a whole number of seconds. Slow
// local models (for example on a CPU) need more than the default.
func llmTimeout(v string) time.Duration {
	if v = strings.TrimSpace(v); v == "" {
		return llm.DefaultTimeout
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		fmt.Fprintf(os.Stderr, "hody: ignoring HODY_LLM_TIMEOUT=%q (want a whole number of seconds); using %s\n", v, llm.DefaultTimeout)
		return llm.DefaultTimeout
	}
	return time.Duration(n) * time.Second
}

// parseInterleaved parses flags that may come before or after positional
// arguments and returns the positionals.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
