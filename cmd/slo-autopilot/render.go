package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/abhisheksoppanna/slo-autopilot/internal/spec"
)

// ---- multi-value -f flag --------------------------------------------------

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// loadAll loads every SLO from every -f path, preserving order.
func loadAll(paths []string) ([]spec.SLO, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one -f <spec.yaml> is required")
	}
	var all []spec.SLO
	for _, p := range paths {
		slos, err := spec.Load(p)
		if err != nil {
			return nil, err
		}
		all = append(all, slos...)
	}
	return all, nil
}

// ---- color ----------------------------------------------------------------

type ansi string

const (
	red    ansi = "31"
	green  ansi = "32"
	yellow ansi = "33"
	cyan   ansi = "36"
	bold   ansi = "1"
	dim    ansi = "2"
)

// colorEnabled is resolved once: honour NO_COLOR and require a terminal.
var colorEnabled = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}()

func color(c ansi, s string) string {
	if !colorEnabled {
		return s
	}
	return "\x1b[" + string(c) + "m" + s + "\x1b[0m"
}

// bar renders a fixed-width progress bar for a 0..1 fraction.
func bar(fraction float64, width int) string {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(fraction*float64(width) + 0.5)
	c := green
	switch {
	case fraction < 0.10:
		c = red
	case fraction < 0.25:
		c = yellow
	}
	return color(c, strings.Repeat("█", filled)) + color(dim, strings.Repeat("░", width-filled))
}
