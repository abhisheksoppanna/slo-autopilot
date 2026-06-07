package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/abhisheksoppanna/slo-autopilot/internal/budget"
	"github.com/abhisheksoppanna/slo-autopilot/internal/burnrate"
	"github.com/abhisheksoppanna/slo-autopilot/internal/prom"
	"github.com/abhisheksoppanna/slo-autopilot/internal/spec"
)

func runBudget(args []string) error {
	fs := flag.NewFlagSet("budget", flag.ContinueOnError)
	var files stringList
	fs.Var(&files, "f", "path to an SLO spec file (repeatable)")
	promURL := fs.String("prometheus", "http://localhost:9090", "Prometheus base URL")
	policyName := fs.String("policy", "standard", "burn-rate policy: standard | fast")
	asJSON := fs.Bool("json", false, "emit machine-readable JSON")
	timeout := fs.Duration("timeout", 15*time.Second, "overall query timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	policy, ok := burnrate.PolicyByName(*policyName)
	if !ok {
		return fmt.Errorf("unknown policy %q", *policyName)
	}
	slos, err := loadAll(files)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	client := prom.New(*promURL)

	statuses, err := evaluateAll(ctx, client, slos, policy)
	if err != nil {
		return err
	}

	if *asJSON {
		return json.NewEncoder(os.Stdout).Encode(statuses)
	}
	for _, st := range statuses {
		printBudget(st)
	}
	return nil
}

// evaluateAll computes the live Status for every SLO.
func evaluateAll(ctx context.Context, q budget.Querier, slos []spec.SLO, p burnrate.Policy) ([]budget.Status, error) {
	out := make([]budget.Status, 0, len(slos))
	for _, s := range slos {
		st, err := budget.Evaluate(ctx, q, s, p)
		if err != nil {
			return nil, fmt.Errorf("evaluate %s: %w", s.Metadata.Name, err)
		}
		out = append(out, st)
	}
	return out, nil
}

func printBudget(st budget.Status) {
	fmt.Printf("\n%s  %s\n", color(bold, st.Name), color(dim, st.Service))
	fmt.Printf("  objective   %g%%   error budget %s\n", st.ObjectivePct, fmtBudget(st.ErrorBudget))
	fmt.Printf("  budget left %s %s\n",
		bar(st.RemainingFraction, 24),
		color(bold, fmt.Sprintf("%5.1f%%", st.RemainingFraction*100)),
	)
	fmt.Printf("  consumed    %.1f%% of budget over the compliance window\n", st.ConsumedFraction*100)
	fmt.Printf("  %s\n", color(dim, "burn rate by window (× budget):"))
	for _, b := range st.Burns {
		marker := color(green, "ok")
		if b.Firing {
			if b.Window.Severity == burnrate.SeverityPage {
				marker = color(red, "PAGE")
			} else {
				marker = color(yellow, "TICKET")
			}
		}
		fmt.Printf("    %-5s %7.2fx  (fires at %4.1fx)  %s\n",
			b.Window.Long, b.BurnRate, b.Window.Factor, marker)
	}
}
