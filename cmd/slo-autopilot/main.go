// Command slo-autopilot turns SLO specs into Prometheus rules, Grafana
// dashboards, a live error-budget report, and a CI/CD release gate.
//
//	slo-autopilot validate -f checkout.slo.yaml
//	slo-autopilot generate -f checkout.slo.yaml --out-dir deploy/generated
//	slo-autopilot budget   -f checkout.slo.yaml --prometheus http://localhost:9090
//	slo-autopilot gate     -f checkout.slo.yaml --prometheus http://localhost:9090
//
// The gate exits non-zero when a service is out of budget or burning fast, so a
// pipeline can simply run it as a step and let the exit code freeze the deploy.
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "validate":
		err = runValidate(args)
	case "generate":
		err = runGenerate(args)
	case "budget":
		err = runBudget(args)
	case "gate":
		err = runGate(args)
	case "version", "--version", "-v":
		fmt.Printf("slo-autopilot %s\n", version)
		return
	case "help", "-h", "--help":
		usage(os.Stdout)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}

	if err != nil {
		// gateBlocked is the one "error" that is an expected outcome, not a
		// tooling failure: exit 1 (blocked) vs exit 2 (misuse/failure).
		if _, blocked := err.(gateBlockedError); blocked {
			fmt.Fprintln(os.Stderr, color(red, "✗ deploy blocked: ")+err.Error())
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, color(red, "error: ")+err.Error())
		os.Exit(2)
	}
}

func usage(w *os.File) {
	fmt.Fprintf(w, `slo-autopilot %s — error-budget-driven release gating for Prometheus

USAGE
  slo-autopilot <command> [flags]

COMMANDS
  validate    Check one or more SLO specs for correctness
  generate    Emit Prometheus rules and a Grafana dashboard from a spec
  budget      Report the live error-budget position from Prometheus
  gate        Decide whether a deploy may proceed (exit 1 = blocked)
  version     Print the version

Run "slo-autopilot <command> -h" for command-specific flags.

EXAMPLES
  slo-autopilot generate -f checkout.slo.yaml --out-dir deploy/generated
  slo-autopilot gate -f checkout.slo.yaml --prometheus http://localhost:9090 --min-remaining 0.1
`, version)
}
