// Package budget computes the live error-budget position for an SLO by querying
// Prometheus. It is the read model behind both the `budget` report and the
// `gate` decision.
package budget

import (
	"context"
	"errors"
	"fmt"

	"github.com/abhisheksoppanna/slo-autopilot/internal/burnrate"
	"github.com/abhisheksoppanna/slo-autopilot/internal/prom"
	"github.com/abhisheksoppanna/slo-autopilot/internal/spec"
)

// Querier is the subset of the Prometheus client this package needs, which
// keeps it trivially mockable in tests.
type Querier interface {
	QueryScalar(ctx context.Context, query string) (float64, error)
}

// WindowBurn is the measured burn rate over one policy window.
type WindowBurn struct {
	Window     burnrate.Window
	ErrorRatio float64 // measured error ratio over the long window
	BurnRate   float64 // ErrorRatio / errorBudget
	Firing     bool    // BurnRate >= Window.Factor (long window breached)
}

// Status is the full live budget position for an SLO.
type Status struct {
	Name         string
	Service      string
	ObjectivePct float64
	ErrorBudget  float64 // allowed error fraction, e.g. 0.001

	// WindowErrorRatio is the error ratio measured over the full compliance
	// window. ConsumedFraction is that as a fraction of the error budget;
	// RemainingFraction is 1 - ConsumedFraction, floored at 0.
	WindowErrorRatio  float64
	ConsumedFraction  float64
	RemainingFraction float64

	Burns []WindowBurn
}

// FastestBurn returns the most urgent firing page-severity burn, if any.
func (s Status) FastestBurn() (WindowBurn, bool) {
	for _, b := range s.Burns {
		if b.Firing && b.Window.Severity == burnrate.SeverityPage {
			return b, true
		}
	}
	return WindowBurn{}, false
}

// Exhausted reports whether the error budget is fully spent.
func (s Status) Exhausted() bool { return s.RemainingFraction <= 0 }

// Evaluate queries Prometheus and assembles the live Status. Windows with no
// data (an idle service) are treated as zero error ratio, not as failures.
func Evaluate(ctx context.Context, q Querier, s spec.SLO, p burnrate.Policy) (Status, error) {
	win, err := s.WindowDuration()
	if err != nil {
		return Status{}, fmt.Errorf("parse window: %w", err)
	}
	eb := s.ErrorBudget()

	st := Status{
		Name:         s.Metadata.Name,
		Service:      s.Metadata.Service,
		ObjectivePct: s.Spec.Objective,
		ErrorBudget:  eb,
	}

	// Budget consumed over the full compliance window.
	windowRatio, err := queryRatio(ctx, q, s.Spec.Indicator, win)
	if err != nil {
		return Status{}, fmt.Errorf("query compliance-window ratio: %w", err)
	}
	st.WindowErrorRatio = windowRatio
	if eb > 0 {
		st.ConsumedFraction = windowRatio / eb
	}
	st.RemainingFraction = 1 - st.ConsumedFraction
	if st.RemainingFraction < 0 {
		st.RemainingFraction = 0
	}

	// Burn rate per policy window (long window only; the short window is the
	// alert's noise filter, not part of the headline burn-rate reading).
	for _, w := range p.Windows {
		ratio, err := queryRatio(ctx, q, s.Spec.Indicator, w.Long)
		if err != nil {
			return Status{}, fmt.Errorf("query %s window ratio: %w", w.Name, err)
		}
		burn := WindowBurn{Window: w, ErrorRatio: ratio}
		if eb > 0 {
			burn.BurnRate = ratio / eb
		}
		burn.Firing = burn.BurnRate >= w.Factor
		st.Burns = append(st.Burns, burn)
	}

	return st, nil
}

func queryRatio(ctx context.Context, q Querier, ind spec.Indicator, w spec.Duration) (float64, error) {
	query := fmt.Sprintf(
		"sum(rate(%s[%s])) / clamp_min(sum(rate(%s[%s])), 1e-9)",
		ind.ErrorMetric, w.Prometheus(), ind.TotalMetric, w.Prometheus(),
	)
	v, err := q.QueryScalar(ctx, query)
	if err != nil {
		if errors.Is(err, prom.ErrNoData) {
			return 0, nil // no traffic yet → no errors
		}
		return 0, err
	}
	if v < 0 {
		v = 0
	}
	return v, nil
}
