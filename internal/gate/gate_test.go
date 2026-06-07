package gate

import (
	"strings"
	"testing"

	"github.com/abhisheksoppanna/slo-autopilot/internal/budget"
	"github.com/abhisheksoppanna/slo-autopilot/internal/burnrate"
)

func healthyStatus() budget.Status {
	return budget.Status{
		Name:              "checkout",
		RemainingFraction: 0.8,
		ConsumedFraction:  0.2,
	}
}

func TestEvaluateAllowsHealthy(t *testing.T) {
	d := Evaluate(healthyStatus(), DefaultPolicy())
	if !d.Allowed {
		t.Fatalf("healthy SLO blocked: %v", d.Reasons)
	}
	if len(d.Reasons) == 0 || !strings.Contains(d.Reasons[0], "healthy") {
		t.Errorf("expected a healthy reason, got %v", d.Reasons)
	}
}

func TestEvaluateBlocksExhausted(t *testing.T) {
	st := budget.Status{Name: "checkout", Window: "30d", RemainingFraction: 0, ConsumedFraction: 1.3}
	d := Evaluate(st, DefaultPolicy())
	if d.Allowed {
		t.Fatal("exhausted budget should block")
	}
	if !strings.Contains(strings.Join(d.Reasons, " "), "exhausted") {
		t.Errorf("expected exhaustion reason, got %v", d.Reasons)
	}
}

func TestEvaluateBlocksBelowMinimum(t *testing.T) {
	st := budget.Status{Name: "checkout", RemainingFraction: 0.05, ConsumedFraction: 0.95}
	p := Policy{MinRemainingFraction: 0.10}
	d := Evaluate(st, p)
	if d.Allowed {
		t.Fatal("5% remaining with 10% minimum should block")
	}
	if !strings.Contains(strings.Join(d.Reasons, " "), "below the required minimum") {
		t.Errorf("expected minimum-budget reason, got %v", d.Reasons)
	}
}

func TestEvaluateBlocksOnFastBurn(t *testing.T) {
	st := healthyStatus() // plenty of budget left...
	st.Burns = []budget.WindowBurn{{
		Window:   burnrate.Window{Name: "1h", Severity: burnrate.SeverityPage, Factor: 14.4},
		BurnRate: 20,
		Firing:   true,
	}}
	d := Evaluate(st, DefaultPolicy())
	if d.Allowed {
		t.Fatal("active page burn should block even with budget remaining")
	}
	if !strings.Contains(strings.Join(d.Reasons, " "), "fast burn") {
		t.Errorf("expected fast-burn reason, got %v", d.Reasons)
	}
}

func TestFastBurnIgnoredWhenDisabled(t *testing.T) {
	st := healthyStatus()
	st.Burns = []budget.WindowBurn{{
		Window:   burnrate.Window{Name: "1h", Severity: burnrate.SeverityPage, Factor: 14.4},
		BurnRate: 20,
		Firing:   true,
	}}
	d := Evaluate(st, Policy{MinRemainingFraction: 0, BlockOnFastBurn: false})
	if !d.Allowed {
		t.Fatalf("fast burn should be ignored when disabled, got %v", d.Reasons)
	}
}

func TestEvaluateAll(t *testing.T) {
	good := healthyStatus()
	bad := budget.Status{Name: "search", RemainingFraction: 0, ConsumedFraction: 1.1}
	decisions, allowed := EvaluateAll([]budget.Status{good, bad}, DefaultPolicy())
	if allowed {
		t.Fatal("one blocked SLO should block the whole deploy")
	}
	if len(decisions) != 2 {
		t.Fatalf("got %d decisions, want 2", len(decisions))
	}
}
