package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rustyeddy/investor/internal/firm"
	"github.com/rustyeddy/investor/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Stdout); err != nil {
		slog.Error("investor", "error", err)
		os.Exit(1)
	}
}

const usage = `Usage: investor <validate|plan|run|serve> [flags]
  -config examples/firm.json
  -workflow research
  -objective 'Research objective'
  -listen 127.0.0.1:8090 (serve only)
Run and service currently use the dry-run runtime.
`

func execute(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, err := fmt.Fprint(out, usage)
		return err
	}
	command := args[0]
	switch command {
	case "validate", "plan", "run", "serve":
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("config", "examples/firm.json", "firm configuration")
	workflow := fs.String("workflow", "research", "workflow ID")
	objective := fs.String("objective", "", "workflow objective")
	address := fs.String("listen", "127.0.0.1:8090", "HTTP listen address")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	c, err := firm.Load(*path)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	e, err := firm.New(c, firm.DryRun{}, nil, "dry-run")
	if err != nil {
		return err
	}
	encode := func(v any) error {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	switch command {
	case "validate":
		return encode(map[string]bool{"valid": true})
	case "plan":
		plan, err := e.Plan(*workflow)
		if err != nil {
			return err
		}
		return encode(plan)
	case "run":
		run, runErr := e.Run(ctx, *workflow, *objective)
		if run.ID != "" {
			if err := encode(run); err != nil {
				return err
			}
		}
		return runErr
	case "serve":
		return serve(ctx, *address, service.Handler(c, e))
	}
	return nil
}

func serve(ctx context.Context, address string, handler http.Handler) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      70 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	slog.Info("investor listening", "address", listener.Addr(), "mode", "dry-run")
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}
