package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rustyeddy/investor/internal/portfolio"
)

// newPortfolioCommand is `investor portfolio`: reading archived experiments in the
// Portfolio repository and archiving runs into an experiment's worktree. All logic
// is in internal/portfolio.
func newPortfolioCommand(stdout io.Writer) *cobra.Command {
	root := os.Getenv(portfolio.RootEnv)
	if root == "" {
		root = portfolio.DefaultRoot
	}
	cmd := &cobra.Command{
		Use:   "portfolio",
		Short: "Read and archive experiments in the Portfolio repository",
		Long: "Lists, summarizes and opens archived experiment packages, verifying every file against the package's\n" +
			"checksums.sha256; only archive writes, and only into an experiment's worktree. See docs/portfolio.md.",
	}
	cmd.PersistentFlags().StringVar(&root, "root", root, "Portfolio checkout or worktree; for archive, the main checkout (default from "+portfolio.RootEnv+")")
	withRepo := func(f func(*portfolio.Repo, []string) error) func(*cobra.Command, []string) error {
		return func(_ *cobra.Command, args []string) error {
			repo, err := portfolio.Open(root)
			if err != nil {
				return err
			}
			defer repo.Close()
			return f(repo, args)
		}
	}
	encode := func(v any) error {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List experiments with their status, completeness and problems",
		Args:  cobra.NoArgs,
		RunE: withRepo(func(repo *portfolio.Repo, _ []string) error {
			all, err := repo.List()
			if err != nil {
				return err
			}
			return printList(stdout, all)
		}),
	}
	summary := &cobra.Command{
		Use:   "summary <experiment-id> [run-id]",
		Short: "Print an experiment, or one of its runs, as JSON",
		Args:  cobra.RangeArgs(1, 2),
		RunE: withRepo(func(repo *portfolio.Repo, args []string) error {
			if len(args) == 1 {
				e, err := repo.Experiment(args[0])
				if err != nil {
					return err
				}
				return encode(e)
			}
			_, run, err := repo.Run(args[0], args[1])
			if err != nil {
				return err
			}
			return encode(run)
		}),
	}
	cat := &cobra.Command{
		Use:   "cat <experiment-id> <run-id> <" + strings.Join(portfolio.Artifacts, "|") + ">",
		Short: "Write one verified Trader file of a run to stdout",
		Args:  cobra.ExactArgs(3),
		RunE: withRepo(func(repo *portfolio.Repo, args []string) error {
			f, err := repo.OpenArtifact(args[0], args[1], args[2])
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(stdout, f)
			return err
		}),
	}
	reconcile := &cobra.Command{
		Use:   "reconcile <experiment-id> <run-id>",
		Short: "Reconcile a run's archived account journal with its recorded summary; print the equity curve as JSON",
		Args:  cobra.ExactArgs(2),
		RunE: withRepo(func(repo *portfolio.Repo, args []string) error {
			curve, err := repo.Reconcile(args[0], args[1])
			if err != nil {
				return err
			}
			return json.NewEncoder(stdout).Encode(curve)
		}),
	}
	var packagePath string
	archive := &cobra.Command{
		Use:   "archive <worktree> <report.json>",
		Short: "Copy a report's finished Trader runs into the experiment's worktree",
		Long: "Copies the report as experiment.json and each Trader run it names, byte for byte, from\n" +
			"<root>/" + portfolio.IngressDir + " into a package in <worktree>, which must be a linked worktree of --root\n" +
			"on branch " + portfolio.BranchPrefix + "<experiment-id>. A new experiment needs --path. A partial package that already\n" +
			"has the report's id is completed without changing anything in it. Nothing is committed.",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			report, err := os.ReadFile(args[1])
			if err != nil {
				return err
			}
			e, rec, err := portfolio.Archive(portfolio.ArchiveRequest{Portfolio: root, Worktree: args[0], Report: report, Path: packagePath})
			if err != nil {
				return err
			}
			return printArchive(stdout, e, rec)
		},
	}
	archive.Flags().StringVar(&packagePath, "path", "", "new package: backtests/<strategy>/<INSTRUMENT>/<Interval>/<experiment>/<run>")
	cmd.AddCommand(list, summary, cat, reconcile, archive)
	return cmd
}

// printList writes one line per experiment, then each experiment's problems.
func printList(w io.Writer, all []portfolio.Experiment) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tEVIDENCE\tSTATUS\tSTRATEGY\tINSTRUMENT\tINTERVAL\tRUNS\tNOTES\tPATH")
	for _, e := range all {
		notes := "missing"
		if e.Notes {
			notes = "present"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%d/%d complete\t%s\t%s\n", orDash(e.ID), e.Evidence, orDash(e.Status),
			orDash(e.Strategy), orDash(e.Instrument), orDash(e.Interval), e.CompleteRunCount(), len(e.Runs), notes, e.Path)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, e := range all {
		for _, p := range e.Problems {
			fmt.Fprintf(w, "%s: %s\n", orDash(e.ID), p)
		}
	}
	return nil
}

// printArchive says what was archived and how complete the package now is.
func printArchive(w io.Writer, e portfolio.Experiment, rec portfolio.ArchiveRecord) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUN\tTRADER STATUS\tCOPIED\tMISSING")
	for _, run := range rec.Runs {
		var copied []string
		for _, f := range run.Files {
			if f.Origin == portfolio.OriginCopied {
				copied = append(copied, f.Name)
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", run.RunID, orDash(run.TraderStatus),
			orDash(strings.Join(copied, ",")), orDash(strings.Join(run.Missing, ",")))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s: evidence %s, %d/%d runs complete, at %s (not committed)\n",
		e.ID, e.Evidence, e.CompleteRunCount(), len(e.Runs), e.Path)
	return err
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
