package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/abhisheksoppanna/slo-autopilot/internal/burnrate"
	"github.com/abhisheksoppanna/slo-autopilot/internal/gate"
	"github.com/abhisheksoppanna/slo-autopilot/internal/prom"
)

// gateBlockedError signals the gate intentionally blocked a deploy. main maps
// it to exit code 1, distinct from exit code 2 used for tooling/usage errors.
type gateBlockedError struct{ n int }

func (e gateBlockedError) Error() string {
	return fmt.Sprintf("%d SLO(s) over budget or burning fast", e.n)
}

func runGate(args []string) error {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	var files stringList
	fs.Var(&files, "f", "path to an SLO spec file (repeatable)")
	promURL := fs.String("prometheus", "http://localhost:9090", "Prometheus base URL")
	policyName := fs.String("policy", "standard", "burn-rate policy: standard | fast")
	minRemaining := fs.Float64("min-remaining", 0, "block if remaining error budget is below this fraction (0..1)")
	blockFastBurn := fs.Bool("block-fast-burn", true, "block when a page-severity burn is active")
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

	gatePolicy := gate.Policy{
		MinRemainingFraction: *minRemaining,
		BlockOnFastBurn:      *blockFastBurn,
	}
	decisions, allowed := gate.EvaluateAll(statuses, gatePolicy)

	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"allowed":   allowed,
			"decisions": decisions,
		})
	} else {
		printGate(decisions, allowed)
	}

	if !allowed {
		n := 0
		for _, d := range decisions {
			if !d.Allowed {
				n++
			}
		}
		return gateBlockedError{n}
	}
	return nil
}

func printGate(decisions []gate.Decision, allowed bool) {
	for _, d := range decisions {
		head := color(green, "ALLOW")
		if !d.Allowed {
			head = color(red, "BLOCK")
		}
		fmt.Printf("\n[%s] %s\n", head, color(bold, d.SLO))
		for _, r := range d.Reasons {
			fmt.Printf("       • %s\n", r)
		}
	}
	fmt.Println()
	if allowed {
		fmt.Println(color(green, "✓ gate: deploy may proceed"))
	} else {
		fmt.Println(color(red, "✗ gate: deploy frozen by error-budget policy"))
	}
}
