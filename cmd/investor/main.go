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
	"github.com/rustyeddy/investor/internal/journal"
	"github.com/rustyeddy/investor/internal/market"
	"github.com/rustyeddy/investor/internal/model"
	"github.com/rustyeddy/investor/internal/service"
	"github.com/rustyeddy/investor/internal/transport"
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
	var configPath, workflow, objective, address, runtimeName, journalDir string

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

	// load is the composition root: it builds the dry-run engine and the
	// service every command uses. Every command validates configuration first.
	// record is true for commands that execute a workflow (run); only those
	// open the journal.
	load := func(runtime string, record bool) (*service.Service, error) {
		c, err := firm.Load(configPath)
		if err != nil {
			return nil, fmt.Errorf("configuration: %w", err)
		}
		var (
			rt   firm.Runtime = firm.DryRun{}
			mode              = "dry-run"
			j    journal.Journal
		)
		dir := journalDir // The flag overrides the config file.
		if dir == "" {
			dir = c.JournalDir
		}
		var m *model.Runtime
		if runtime == "model" {
			// Paid, model-backed research must leave a durable record.
			if dir == "" {
				return nil, errors.New("model runtime requires a journal: set journal_dir in the config file or pass --journal")
			}
			if m, err = model.New(c, os.Getenv); err != nil {
				return nil, fmt.Errorf("model runtime: %w", err)
			}
			rt, mode = m, "research"
		}
		if record && dir != "" {
			if j, err = journal.Open(dir); err != nil {
				return nil, err
			}
		}
		if m != nil {
			m.Journal = j
		}
		e, err := firm.New(c, rt, nil, mode)
		if err != nil {
			return nil, err
		}
		if j != nil {
			e.WithObserver(journal.NewObserver(j))
		}
		return service.New(c, e), nil
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
			if _, err := load("dry-run", false); err != nil {
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
			app, err := load("dry-run", false)
			if err != nil {
				return err
			}
			agents, err := app.Plan(workflow)
			if err != nil {
				return err
			}
			return encode(agents)
		},
	}
	plan.Flags().StringVar(&workflow, "workflow", "research", "workflow ID")

	run := &cobra.Command{
		Use:   "run",
		Short: "Run a workflow (dry-run by default, or with LLMs via --runtime model)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtimeName != "dry-run" && runtimeName != "model" {
				return fmt.Errorf("unknown runtime %q (want dry-run or model)", runtimeName)
			}
			app, err := load(runtimeName, true)
			if err != nil {
				return err
			}
			result, runErr := app.Run(cmd.Context(), workflow, objective)
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
	run.Flags().StringVar(&journalDir, "journal", "", "research journal directory, overriding journal_dir in the config file; required for --runtime model")
	run.Flags().StringVar(&runtimeName, "runtime", "dry-run", `runtime: "dry-run" or "model" (calls the LLMs configured in the config file)`)

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the HTTP API in the foreground",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			app, err := load("dry-run", false)
			if err != nil {
				return err
			}
			return serve(cmd.Context(), address, transport.Handler(app))
		},
	}
	serveCmd.Flags().StringVar(&address, "listen", "127.0.0.1:8090", "HTTP listen address")

	// source builds Trader's market-data client from the configuration.
	source := func(traderURL string) (market.Source, *firm.Config, error) {
		c, err := firm.Load(configPath)
		if err != nil {
			return nil, nil, fmt.Errorf("configuration: %w", err)
		}
		t := firm.Trader{BaseURL: traderURL, TokenEnv: "TRADER_API_TOKEN"}
		if c.Trader != nil {
			t = *c.Trader
			if t.TokenEnv == "" {
				t.TokenEnv = "TRADER_API_TOKEN"
			}
			if traderURL != "" {
				t.BaseURL = traderURL
			}
		}
		if t.BaseURL == "" {
			return nil, nil, errors.New("no Trader API: set trader.base_url in the config file or pass --trader")
		}
		return &market.Client{BaseURL: t.BaseURL, Token: os.Getenv(t.TokenEnv)}, &c, nil
	}

	var (
		traderURL, instrument, interval, from, to, asOf string
		strict                                          bool
	)
	bars := &cobra.Command{
		Use:   "bars",
		Short: "Read bars from Trader's market-data API",
		Long:  "Read completed bars from Trader, with coverage and a fingerprint of the data. Times are RFC 3339 or YYYY-MM-DD (UTC); --to is exclusive.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			src, _, err := source(traderURL)
			if err != nil {
				return err
			}
			r := market.Request{Instrument: instrument, Interval: interval, Strict: strict}
			for _, p := range []struct {
				flag, val string
				dst       *time.Time
			}{{"from", from, &r.From}, {"to", to, &r.To}, {"as-of", asOf, &r.AsOf}} {
				if p.val == "" {
					continue
				}
				if *p.dst, err = parseTime(p.val); err != nil {
					return fmt.Errorf("--%s: %w", p.flag, err)
				}
			}
			got, err := src.Bars(cmd.Context(), r)
			if err != nil {
				return err
			}
			return encode(got)
		},
	}
	bars.Flags().StringVar(&traderURL, "trader", "", "Trader API base URL, overriding trader.base_url")
	bars.Flags().StringVar(&instrument, "instrument", "", "instrument, e.g. EURUSD")
	bars.Flags().StringVar(&interval, "interval", "d1", "h1 or d1")
	bars.Flags().StringVar(&from, "from", "", "start (inclusive)")
	bars.Flags().StringVar(&to, "to", "", "end (exclusive)")
	bars.Flags().StringVar(&asOf, "as-of", "", "only bars available at this time (no look-ahead)")
	bars.Flags().BoolVar(&strict, "strict", false, "fail if the range is not complete")

	snapshot := &cobra.Command{
		Use:   "snapshot",
		Short: "Print the latest completed D1 and H1 bar of the configured watchlist",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			src, c, err := source(traderURL)
			if err != nil {
				return err
			}
			if c.Trader == nil || len(c.Trader.Watchlist) == 0 {
				return errors.New("no watchlist: set trader.watchlist in the config file")
			}
			return encode(market.TakeSnapshot(cmd.Context(), src, c.Trader.Watchlist, time.Now()))
		},
	}
	snapshot.Flags().StringVar(&traderURL, "trader", "", "Trader API base URL, overriding trader.base_url")

	wfaCmd := newWFACommand(stdout, func(traderURL string) (market.Source, error) {
		src, _, err := source(traderURL)
		return src, err
	})
	root.AddCommand(validate, plan, run, serveCmd, bars, snapshot, wfaCmd)
	return root
}

// parseTime accepts an RFC 3339 time or a YYYY-MM-DD UTC date.
func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse(time.DateOnly, s)
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
