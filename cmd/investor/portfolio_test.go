package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rustyeddy/investor/internal/portfolio"
)

const (
	testPortfolio = "../../internal/portfolio/testdata/portfolio"
	wfa01         = "wfa01-emax-eurusd-h1-run1"
	wfa01OOS      = "run_01M8600S0W2PX33XJXVGXV770B"
)

func portfolioCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := execute(context.Background(), append([]string{"portfolio", "--root", testPortfolio}, args...), &out, &errOut)
	return out.String(), err
}

func TestPortfolioList(t *testing.T) {
	out, err := portfolioCmd(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{wfa01, "wfa02-emax-eurusd-h1-run1", "partial", "0/9 complete", "backtests/emax/EURUSD/H1/wfa01/run1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list output lacks %q:\n%s", want, out)
		}
	}
}

func TestPortfolioSummaryAndCat(t *testing.T) {
	out, err := portfolioCmd(t, "summary", wfa01, wfa01OOS)
	if err != nil {
		t.Fatal(err)
	}
	var run portfolio.Run
	if err := json.Unmarshal([]byte(out), &run); err != nil || run.RunID != wfa01OOS || run.Evidence != portfolio.Partial {
		t.Fatalf("summary = %s, err = %v", out, err)
	}
	out, err = portfolioCmd(t, "cat", wfa01, wfa01OOS, portfolio.RunFile)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(out))
	for _, f := range run.Files {
		if f.Name == portfolio.RunFile && f.State != portfolio.Verified {
			t.Fatalf("run.json is %s", f.State)
		}
	}
	if !strings.HasPrefix(hex.EncodeToString(sum[:]), "35825657") {
		t.Fatalf("cat run.json hashed to %x", sum)
	}
}

func TestPortfolioRefusesMissingEvidence(t *testing.T) {
	for _, args := range [][]string{
		{"cat", wfa01, wfa01OOS, portfolio.AccountFile},
		{"reconcile", wfa01, wfa01OOS},
		{"summary", "no-such-experiment"},
		{"cat", wfa01, wfa01OOS, "../experiment.json"},
	} {
		if out, err := portfolioCmd(t, args...); err == nil || out != "" {
			t.Fatalf("%v: output %q, err %v", args, out, err)
		}
	}
}
