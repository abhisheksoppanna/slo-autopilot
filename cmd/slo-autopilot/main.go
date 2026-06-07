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
	os.Exit(run(os.Args[1:]))
}

// run dispatches a single command and returns the process exit code:
//
//	0 = success, 1 = gate blocked by error-budget policy, 2 = usage/tooling error.
//
// Keeping the exit-code logic here (rather than calling os.Exit inline) makes
// the gate's allow/block/misuse contract — the thing a CI pipeline keys off —
// unit-testable.
func run(args []string) int {
	if len(args) < 1 {
		usage(os.Stderr)
		return 2
	}

	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "validate":
		err = runValidate(rest)
	case "generate":
		err = runGenerate(rest)
	case "budget":
		err = runBudget(rest)
	case "gate":
		err = runGate(rest)
	case "version", "--version", "-v":
		fmt.Printf("slo-autopilot %s\n", version)
		return 0
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return 2
	}

	if err != nil {
		// gateBlockedError is the one "error" that is an expected outcome, not a
		// tooling failure: exit 1 (blocked) vs exit 2 (misuse/failure).
		if _, blocked := err.(gateBlockedError); blocked {
			fmt.Fprintln(os.Stderr, color(red, "✗ deploy blocked: ")+err.Error())
			return 1
		}
		fmt.Fprintln(os.Stderr, color(red, "error: ")+err.Error())
		return 2
	}
	return 0
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
