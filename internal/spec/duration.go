package spec

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Duration is a time span that, unlike time.Duration, round-trips the day (d)
// and week (w) units used throughout SLO and Prometheus configuration.
type Duration time.Duration

const (
	day  = Duration(24 * time.Hour)
	week = 7 * day
)

// ParseDuration parses a Prometheus-style duration such as "5m", "1h", "30d"
// or "4w". Compound forms ("1h30m") are supported for the time-package units.
func ParseDuration(s string) (Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// Fast path: a single day/week token, e.g. "30d" or "4w".
	if d, ok := parseUnitSuffix(s, "d", day); ok {
		return d, nil
	}
	if d, ok := parseUnitSuffix(s, "w", week); ok {
		return d, nil
	}

	// Fall back to the standard library for h/m/s and compounds.
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("not a valid duration (use forms like 5m, 1h, 30d, 4w): %q", s)
	}
	return Duration(d), nil
}

func parseUnitSuffix(s, suffix string, unit Duration) (Duration, bool) {
	if !strings.HasSuffix(s, suffix) {
		return 0, false
	}
	n, err := strconv.ParseFloat(strings.TrimSuffix(s, suffix), 64)
	if err != nil {
		return 0, false
	}
	return Duration(float64(unit) * n), true
}

// Std returns the value as a standard time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Prometheus renders the duration in Prometheus range-vector syntax, preferring
// the largest whole unit (so 86400s becomes "1d", not "24h"). Prometheus does
// not accept compound range selectors, so non-whole values fall back to seconds.
func (d Duration) Prometheus() string {
	td := time.Duration(d)
	switch {
	case td <= 0:
		return "0s"
	case td%time.Duration(week) == 0:
		return fmt.Sprintf("%dw", td/time.Duration(week))
	case td%time.Duration(day) == 0:
		return fmt.Sprintf("%dd", td/time.Duration(day))
	case td%time.Hour == 0:
		return fmt.Sprintf("%dh", td/time.Hour)
	case td%time.Minute == 0:
		return fmt.Sprintf("%dm", td/time.Minute)
	default:
		return fmt.Sprintf("%ds", td/time.Second)
	}
}

// String renders a compact, human-friendly form for CLI output.
func (d Duration) String() string { return d.Prometheus() }
