package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/investor/internal/market"
	"github.com/rustyeddy/investor/internal/wfa"
)

// newWFACommand is `investor wfa`. It only turns flags into arguments for
// internal/wfa and prints what comes back. The Trader address comes from the
// spec's trader_api, not the firm configuration.
func newWFACommand(stdout io.Writer) *cobra.Command {
	var specPath, outDir, traderURL string
	var skipPreflight bool
	wfaCmd := &cobra.Command{Use: "wfa", Short: "Walk-forward analysis experiments"}
	run := &cobra.Command{
		Use:   "run",
		Short: "Run one walk-forward experiment: train grid, choose, one out-of-sample run",
		Long: "Backtests every grid configuration on the training window through the standalone strategy and a running\n" +
			"Trader, chooses one from training results alone, runs it once out of sample, and writes <out>/<id>.json.\n" +
			"See docs/wfa.md.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spec, err := wfa.LoadSpec(specPath)
			if err != nil {
				return err
			}
			var src market.Source
			if !skipPreflight {
				if src, err = spec.Source(traderURL, os.Getenv); err != nil {
					return err
				}
			}
			rep, path, runErr := wfa.RunExperiment(cmd.Context(), spec, src, outDir)
			if path != "" {
				fmt.Fprint(stdout, rep.Summary())
				fmt.Fprintf(stdout, "report: %s\n", path)
			}
			return runErr
		},
	}
	f := run.Flags()
	f.StringVar(&specPath, "spec", "", "experiment spec file (required)")
	f.StringVar(&outDir, "out", "wfa-reports", "directory for the report")
	f.StringVar(&traderURL, "trader", "", "Trader API base URL for the data preflight, overriding trader_api.base_url in the spec")
	f.BoolVar(&skipPreflight, "skip-preflight", false, "skip the data preflight (recorded in the report)")
	_ = run.MarkFlagRequired("spec")
	wfaCmd.AddCommand(run, newWFAReconcileCommand(stdout))
	return wfaCmd
}

func newWFAReconcileCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile <investor-report> <run-id>",
		Short: "Validate a supplied account journal on stdin and print the equity curve as JSON",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			curve, err := wfa.ReconcileRun(args[0], args[1], cmd.InOrStdin())
			if err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(curve)
		},
	}
}
