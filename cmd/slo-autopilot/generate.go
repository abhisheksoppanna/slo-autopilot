package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/abhisheksoppanna/slo-autopilot/internal/burnrate"
	"github.com/abhisheksoppanna/slo-autopilot/internal/dashboard"
	"github.com/abhisheksoppanna/slo-autopilot/internal/promrules"
)

func runGenerate(args []string) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	var files stringList
	fs.Var(&files, "f", "path to an SLO spec file (repeatable)")
	policyName := fs.String("policy", "standard", "burn-rate policy: standard | fast")
	outDir := fs.String("out-dir", "", "write <name>.rules.yaml and <name>.dashboard.json here (default: rules to stdout)")
	what := fs.String("only", "all", "what to emit: all | rules | dashboard")
	if err := fs.Parse(args); err != nil {
		return err
	}

	policy, ok := burnrate.PolicyByName(*policyName)
	if !ok {
		return fmt.Errorf("unknown policy %q (use standard or fast)", *policyName)
	}

	slos, err := loadAll(files)
	if err != nil {
		return err
	}

	if *outDir == "" {
		// stdout mode: rules only, so the output is pipeable into a file.
		if *what == "dashboard" {
			return fmt.Errorf("--only dashboard requires --out-dir")
		}
		for _, s := range slos {
			rules := promrules.Generate(s, policy)
			out, err := promrules.Marshal(rules)
			if err != nil {
				return err
			}
			if _, err := os.Stdout.Write(out); err != nil {
				return err
			}
		}
		return nil
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fmt.Errorf("create out-dir: %w", err)
	}

	for _, s := range slos {
		if *what == "all" || *what == "rules" {
			rules := promrules.Generate(s, policy)
			out, err := promrules.Marshal(rules)
			if err != nil {
				return err
			}
			path := filepath.Join(*outDir, s.Metadata.Name+".rules.yaml")
			if err := os.WriteFile(path, out, 0o644); err != nil {
				return fmt.Errorf("write rules: %w", err)
			}
			fmt.Printf("%s  %s\n", color(green, "✓ rules    "), path)
		}
		if *what == "all" || *what == "dashboard" {
			dash, err := dashboard.Generate(s, policy)
			if err != nil {
				return err
			}
			path := filepath.Join(*outDir, s.Metadata.Name+".dashboard.json")
			if err := os.WriteFile(path, dash, 0o644); err != nil {
				return fmt.Errorf("write dashboard: %w", err)
			}
			fmt.Printf("%s  %s\n", color(green, "✓ dashboard"), path)
		}
	}
	fmt.Printf("%s policy=%s, %d SLO(s)\n", color(dim, "done:"), policy.Name, len(slos))
	return nil
}
