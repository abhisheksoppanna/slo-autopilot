// Package burnrate implements multi-window, multi-burn-rate alerting policy as
// described in the Google SRE Workbook, chapter "Alerting on SLOs".
//
// The idea: instead of paging on a raw error-rate threshold (which is either
// too noisy or too slow), page on the *burn rate* — how fast the error budget
// is being consumed relative to the budget for the whole compliance window. A
// burn rate of 1 exactly exhausts the budget over the window; a burn rate of 14.4
// exhausts it in 1/14.4 of the window. Pairing a long window (signal) with a
// short window (fast reset) catches both sustained and sudden burns while
// keeping false pages low.
package burnrate

import (
	"time"

	"github.com/abhisheksoppanna/slo-autopilot/internal/spec"
)

// Severity controls how an alert is routed: page now, or file a ticket.
type Severity string

const (
	SeverityPage   Severity = "page"
	SeverityTicket Severity = "ticket"
)

// Window is one burn-rate alerting condition. The alert fires only when the
// measured error ratio exceeds Factor*budget over BOTH Long and Short windows.
type Window struct {
	// Name is a short label used in generated rule names, e.g. "1h".
	Name string
	// Severity selects routing (page vs. ticket).
	Severity Severity
	// Long is the slow signal window; Short is the fast-reset window.
	Long  spec.Duration
	Short spec.Duration
	// Factor is the burn-rate multiplier applied to the error budget.
	Factor float64
}

// BudgetConsumed returns the fraction of the total error budget that is spent
// if this burn rate is sustained for the whole Long window.
func (w Window) BudgetConsumed(complianceWindow spec.Duration) float64 {
	if complianceWindow <= 0 {
		return 0
	}
	return w.Factor * float64(w.Long) / float64(complianceWindow)
}

// Policy is an ordered set of burn-rate windows, most-urgent first.
type Policy struct {
	// Name identifies the policy in CLI output and docs.
	Name    string
	Windows []Window
}

// PolicyByName returns a named policy, or false if unknown.
func PolicyByName(name string) (Policy, bool) {
	switch name {
	case "", "standard":
		return Standard(), true
	case "fast", "demo":
		return FastDemo(), true
	default:
		return Policy{}, false
	}
}

// Standard is the canonical SRE Workbook policy, tuned for a 30-day compliance
// window. The factors (14.4, 6, 3, 1) are the book's recommended values.
//
//	severity  long  short  factor  budget burned if sustained
//	page       1h    5m    14.4    2%   over 1h
//	page       6h   30m     6      5%   over 6h
//	ticket     1d    2h     3      10%  over 1d
//	ticket     3d    6h     1      10%  over 3d
func Standard() Policy {
	return Policy{
		Name: "standard",
		Windows: []Window{
			{Name: "1h", Severity: SeverityPage, Long: hours(1), Short: minutes(5), Factor: 14.4},
			{Name: "6h", Severity: SeverityPage, Long: hours(6), Short: minutes(30), Factor: 6},
			{Name: "1d", Severity: SeverityTicket, Long: hours(24), Short: hours(2), Factor: 3},
			{Name: "3d", Severity: SeverityTicket, Long: hours(72), Short: hours(6), Factor: 1},
		},
	}
}

// FastDemo is a time-compressed policy so a full burn — and a blocked deploy —
// can be observed within a few minutes during `docker compose up`. The window
// ratios mirror Standard (page-fast burns the budget over the long window,
// etc.) but are scaled down ~12x. Never use this in production.
func FastDemo() Policy {
	return Policy{
		Name: "fast",
		Windows: []Window{
			{Name: "5m", Severity: SeverityPage, Long: minutes(5), Short: minutes(1), Factor: 14.4},
			{Name: "15m", Severity: SeverityPage, Long: minutes(15), Short: minutes(3), Factor: 6},
			{Name: "1h", Severity: SeverityTicket, Long: hours(1), Short: minutes(10), Factor: 3},
			{Name: "3h", Severity: SeverityTicket, Long: hours(3), Short: minutes(30), Factor: 1},
		},
	}
}

// DistinctWindows returns every Long/Short duration referenced by the policy,
// deduplicated. The rule generator emits one recording rule per distinct window
// so alert expressions stay cheap and dashboards can reuse them.
func (p Policy) DistinctWindows() []spec.Duration {
	seen := map[spec.Duration]bool{}
	var out []spec.Duration
	add := func(d spec.Duration) {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	for _, w := range p.Windows {
		add(w.Long)
		add(w.Short)
	}
	// Sort ascending for stable, readable output.
	sortDurations(out)
	return out
}

func hours(n int) spec.Duration   { return spec.Duration(time.Duration(n) * time.Hour) }
func minutes(n int) spec.Duration { return spec.Duration(time.Duration(n) * time.Minute) }

func sortDurations(ds []spec.Duration) {
	// Small slices; insertion sort keeps it dependency-free and stable.
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && ds[j-1] > ds[j]; j-- {
			ds[j-1], ds[j] = ds[j], ds[j-1]
		}
	}
}
