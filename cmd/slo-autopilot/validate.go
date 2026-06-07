package main

import (
	"flag"
	"fmt"
)

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	var files stringList
	fs.Var(&files, "f", "path to an SLO spec file (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Load already validates; a returned error means at least one spec is bad.
	slos, err := loadAll(files)
	if err != nil {
		return err
	}

	fmt.Println(color(bold, fmt.Sprintf("✓ %d SLO(s) valid", len(slos))))
	for _, s := range slos {
		d, _ := s.WindowDuration()
		fmt.Printf("  %s  %s  objective %s over %s  (budget %s)\n",
			color(cyan, s.Metadata.Name),
			color(dim, s.Metadata.Service),
			color(bold, fmt.Sprintf("%g%%", s.Spec.Objective)),
			d,
			fmtBudget(s.ErrorBudget()),
		)
	}
	return nil
}

// fmtBudget renders an error budget fraction as a percentage, e.g. 0.001 → "0.1%".
// The %.4g precision rounds away binary floating-point noise (0.001*100 would
// otherwise print as 0.09999999999998899).
func fmtBudget(b float64) string {
	return fmt.Sprintf("%.4g%%", b*100)
}
