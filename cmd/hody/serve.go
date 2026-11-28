package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"hody/internal/model"
	"hody/internal/pipeline"
	"hody/internal/store"
	"hody/internal/web"
)

func cmdServe(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	commandUsage(fs, "hody serve [options]", "Opens the local web page, where you can start a diagnosis and read past ones.\nIt listens on 127.0.0.1 only.")
	addr := fs.String("addr", "127.0.0.1:8080", "listen address; must be 127.0.0.1")
	var o diagnoseOpts
	fs.StringVar(&o.socket, "socket", defaultSocket(), "Docker socket path")
	fs.StringVar(&o.kubeconfig, "kubeconfig", "", "kubeconfig path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := checkLoopback(*addr); err != nil {
		return err
	}
	st, err := store.Open(dataDir())
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	run := func(ctx context.Context, s *model.Session, notify func(model.Session)) error {
		o := o
		o.file = s.Target.File
		deps, err := liveDeps(s.Target, o, st, filepath.Join(st.Dir(), "llm-requests", s.ID))
		if err != nil {
			s.Status, s.Error = model.StatusFailed, err.Error()
			_ = st.Save(s)
			return err
		}
		deps.Notify = notify
		return pipeline.New(deps).Run(ctx, s)
	}
	srv, err := web.New(ctx, *addr, st, run)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(shut)
	}()
	fmt.Fprintf(stderr, "Hody is running at http://%s (press Ctrl+C to stop)\n", *addr)
	if err := hs.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// checkLoopback allows only 127.0.0.1 with a port, as the specification
// requires the page to listen on 127.0.0.1 only.
func checkLoopback(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return fmt.Errorf("--addr must look like 127.0.0.1:8080")
	}
	if host != "127.0.0.1" {
		return fmt.Errorf("Hody only listens on 127.0.0.1, not %q", host)
	}
	return nil
}
