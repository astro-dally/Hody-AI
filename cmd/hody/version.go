package main

import (
	"fmt"
	"runtime"
)

// Set at build time by the release build:
// -ldflags "-X main.version=v0.1.0 -X main.commit=<sha> -X main.date=<rfc3339>"
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func versionLine() string {
	return fmt.Sprintf("hody %s (commit %s, built %s, %s, %s/%s)", version, commit, date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
