package burnrate

import (
	"math"
	"testing"

	"github.com/abhisheksoppanna/slo-autopilot/internal/spec"
)

func TestStandardBudgetConsumed(t *testing.T) {
	// These are the canonical SRE Workbook figures for a 30-day window.
	window, _ := spec.ParseDuration("30d")
	p := Standard()

	want := map[string]float64{
		"1h": 0.02, // 2% of budget burned over 1h at 14.4x
		"6h": 0.05, // 5% over 6h at 6x
		"1d": 0.10, // 10% over 1d at 3x
		"3d": 0.10, // 10% over 3d at 1x
	}
	for _, w := range p.Windows {
		got := w.BudgetConsumed(window)
		if math.Abs(got-want[w.Name]) > 1e-6 {
			t.Errorf("window %s: budget consumed = %.4f, want %.4f", w.Name, got, want[w.Name])
		}
	}
}

func TestDistinctWindowsSortedAndDeduped(t *testing.T) {
	p := Standard()
	ds := p.DistinctWindows()

	// Standard has windows {5m,1h,30m,6h,2h,1d,6h...} → distinct, sorted asc.
	if len(ds) == 0 {
		t.Fatal("no distinct windows")
	}
	for i := 1; i < len(ds); i++ {
		if ds[i-1] >= ds[i] {
			t.Errorf("distinct windows not strictly ascending at %d: %v", i, ds)
		}
	}
	// 6h appears as both a Long (6h window) and a Short (3d window) → must dedupe.
	count6h := 0
	sixHours, _ := spec.ParseDuration("6h")
	for _, d := range ds {
		if d == sixHours {
			count6h++
		}
	}
	if count6h != 1 {
		t.Errorf("6h appears %d times, want exactly 1 (dedup failed)", count6h)
	}
}

func TestPolicyByName(t *testing.T) {
	cases := map[string]string{
		"":         "standard",
		"standard": "standard",
		"fast":     "fast",
		"demo":     "fast",
	}
	for in, wantName := range cases {
		p, ok := PolicyByName(in)
		if !ok {
			t.Errorf("PolicyByName(%q): not found", in)
			continue
		}
		if p.Name != wantName {
			t.Errorf("PolicyByName(%q).Name = %q, want %q", in, p.Name, wantName)
		}
	}
	if _, ok := PolicyByName("nonsense"); ok {
		t.Error("PolicyByName(nonsense): expected not found")
	}
}
