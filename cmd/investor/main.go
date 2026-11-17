package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/investor/internal/firm"
	"github.com/rustyeddy/investor/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		slog.Error("investor", "error", err)
		os.Exit(1)
	}
}

// execute runs the CLI with args. Results and help go to stdout; stderr is
// reserved for diagnostics. Errors are returned, not printed, so main reports
// them once.
func execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cmd := newRootCommand(stdout, stderr)
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

func newRootCommand(stdout, stderr io.Writer) *cobra.Command {
	var configPath, workflow, objective, address string

	root := &cobra.Command{
		Use:           "investor",
		Short:         "AI-assisted trading research firm orchestrator",
		Long:          "Run and service currently use the dry-run runtime.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVar(&configPath, "config", "examples/firm.json", "firm configuration")

	// load builds the dry-run engine; every command validates configuration first.
	load := func() (firm.Config, *firm.Engine, error) {
		c, err := firm.Load(configPath)
		if err != nil {
			return c, nil, fmt.Errorf("configuration: %w", err)
		}
		e, err := firm.New(c, firm.DryRun{}, nil, "dry-run")
		return c, e, err
	}
	encode := func(v any) error {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}

	validate := &cobra.Command{
		Use:   "validate",
		Short: "Validate the firm configuration and its skills",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, _, err := load(); err != nil {
				return err
			}
			return encode(map[string]bool{"valid": true})
		},
	}

	plan := &cobra.Command{
		Use:   "plan",
		Short: "Print the ordered agents of a workflow",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			_, e, err := load()
			if err != nil {
				return err
			}
			agents, err := e.Plan(workflow)
			if err != nil {
				return err
			}
			return encode(agents)
		},
	}
	plan.Flags().StringVar(&workflow, "workflow", "research", "workflow ID")

	run := &cobra.Command{
		Use:   "run",
		Short: "Run a workflow with the dry-run runtime",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, e, err := load()
			if err != nil {
				return err
			}
			result, runErr := e.Run(cmd.Context(), workflow, objective)
			if result.ID != "" {
				if err := encode(result); err != nil {
					return err
				}
			}
			return runErr
		},
	}
	run.Flags().StringVar(&workflow, "workflow", "research", "workflow ID")
	run.Flags().StringVar(&objective, "objective", "", "workflow objective")

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the HTTP API in the foreground",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, e, err := load()
			if err != nil {
				return err
			}
			return serve(cmd.Context(), address, service.Handler(c, e))
		},
	}
	serveCmd.Flags().StringVar(&address, "listen", "127.0.0.1:8090", "HTTP listen address")

	root.AddCommand(validate, plan, run, serveCmd)
	return root
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
