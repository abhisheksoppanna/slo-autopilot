package budget

import (
	"context"
	"strings"
	"testing"

	"github.com/abhisheksoppanna/slo-autopilot/internal/burnrate"
	"github.com/abhisheksoppanna/slo-autopilot/internal/prom"
	"github.com/abhisheksoppanna/slo-autopilot/internal/spec"
)

// fakeQuerier answers based on the range window present in the query string,
// so we can simulate different error ratios over different windows.
type fakeQuerier struct {
	// byWindow maps a Prometheus range token (e.g. "30d", "1h") to an error ratio.
	byWindow map[string]float64
	noData   bool
}

func (f fakeQuerier) QueryScalar(_ context.Context, query string) (float64, error) {
	if f.noData {
		return 0, prom.ErrNoData
	}
	for win, val := range f.byWindow {
		if strings.Contains(query, "["+win+"]") {
			return val, nil
		}
	}
	return 0, nil
}

func testSLO() spec.SLO {
	return spec.SLO{
		APIVersion: spec.APIVersion,
		Metadata:   spec.Metadata{Name: "checkout", Service: "checkout-api"},
		Spec: spec.Spec{
			Objective: 99.9, // budget 0.001
			Window:    "30d",
			Indicator: spec.Indicator{
				Type:        spec.IndicatorRatio,
				ErrorMetric: `http_requests_total{code=~"5.."}`,
				TotalMetric: `http_requests_total`,
			},
		},
	}
}

func TestEvaluateHealthy(t *testing.T) {
	q := fakeQuerier{byWindow: map[string]float64{
		"30d": 0.0002, // 20% of budget consumed
		"1h":  0.0,
		"6h":  0.0,
		"1d":  0.0,
		"3d":  0.0,
	}}
	st, err := Evaluate(context.Background(), q, testSLO(), burnrate.Standard())
	if err != nil {
		t.Fatal(err)
	}
	if got := round(st.ConsumedFraction, 4); got != 0.2 {
		t.Errorf("consumed = %v, want 0.2", got)
	}
	if got := round(st.RemainingFraction, 4); got != 0.8 {
		t.Errorf("remaining = %v, want 0.8", got)
	}
	if st.Exhausted() {
		t.Error("healthy SLO reported as exhausted")
	}
	if _, firing := st.FastestBurn(); firing {
		t.Error("healthy SLO has a firing page burn")
	}
}

func TestEvaluateFastBurn(t *testing.T) {
	// Error ratio 0.02 over the 1h window = 20x budget → above the 14.4x page
	// threshold, so the 1h page window should fire.
	q := fakeQuerier{byWindow: map[string]float64{
		"30d": 0.0005,
		"1h":  0.02,
		"6h":  0.02,
		"1d":  0.02,
		"3d":  0.02,
	}}
	st, err := Evaluate(context.Background(), q, testSLO(), burnrate.Standard())
	if err != nil {
		t.Fatal(err)
	}
	b, firing := st.FastestBurn()
	if !firing {
		t.Fatal("expected a firing page burn")
	}
	if b.Window.Name != "1h" {
		t.Errorf("fastest burn window = %q, want 1h", b.Window.Name)
	}
	if round(b.BurnRate, 2) != 20 {
		t.Errorf("burn rate = %v, want 20", round(b.BurnRate, 2))
	}
}

func TestEvaluateExhausted(t *testing.T) {
	q := fakeQuerier{byWindow: map[string]float64{
		"30d": 0.0015, // 150% of budget
	}}
	st, err := Evaluate(context.Background(), q, testSLO(), burnrate.Standard())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Exhausted() {
		t.Errorf("expected exhausted, remaining = %v", st.RemainingFraction)
	}
	if st.RemainingFraction != 0 {
		t.Errorf("remaining should floor at 0, got %v", st.RemainingFraction)
	}
}

func TestEvaluateNoData(t *testing.T) {
	st, err := Evaluate(context.Background(), fakeQuerier{noData: true}, testSLO(), burnrate.Standard())
	if err != nil {
		t.Fatal(err)
	}
	if st.ConsumedFraction != 0 || st.RemainingFraction != 1 {
		t.Errorf("idle service should have full budget, got consumed=%v remaining=%v",
			st.ConsumedFraction, st.RemainingFraction)
	}
}

func TestEvaluateNegativeRatioClamped(t *testing.T) {
	// A negative measured ratio (defensive — shouldn't happen) clamps to 0, so
	// the budget never appears to refill and no window spuriously fires.
	q := fakeQuerier{byWindow: map[string]float64{
		"30d": -0.5, "1h": -0.5, "6h": -0.5, "1d": -0.5, "3d": -0.5,
	}}
	st, err := Evaluate(context.Background(), q, testSLO(), burnrate.Standard())
	if err != nil {
		t.Fatal(err)
	}
	if st.ConsumedFraction != 0 || st.RemainingFraction != 1 {
		t.Errorf("negative ratio should clamp to 0 consumed, got consumed=%v remaining=%v",
			st.ConsumedFraction, st.RemainingFraction)
	}
	for _, b := range st.Burns {
		if b.BurnRate != 0 || b.Firing {
			t.Errorf("window %s: negative ratio should yield 0 burn, not firing", b.Window.Name)
		}
	}
}

func TestBurnFiresAtExactlyFactorBoundary(t *testing.T) {
	// Firing is BurnRate >= Factor (boundary inclusive). At the 1h page factor
	// (14.4x budget = 0.0144 error ratio) the window must fire.
	q := fakeQuerier{byWindow: map[string]float64{
		"30d": 0.0001, "1h": 0.0144, "6h": 0.0, "1d": 0.0, "3d": 0.0,
	}}
	st, err := Evaluate(context.Background(), q, testSLO(), burnrate.Standard())
	if err != nil {
		t.Fatal(err)
	}
	var oneH *WindowBurn
	for i := range st.Burns {
		if st.Burns[i].Window.Name == "1h" {
			oneH = &st.Burns[i]
		}
	}
	if oneH == nil {
		t.Fatal("no 1h burn in status")
	}
	if got := round(oneH.BurnRate, 2); got != 14.4 {
		t.Errorf("1h burn rate = %v, want 14.4", got)
	}
	if !oneH.Firing {
		t.Error("burn rate at exactly the Factor boundary should fire (>=)")
	}
}

func round(f float64, places int) float64 {
	p := 1.0
	for i := 0; i < places; i++ {
		p *= 10
	}
	return float64(int64(f*p+0.5)) / p
}
