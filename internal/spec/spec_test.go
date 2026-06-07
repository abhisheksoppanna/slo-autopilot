package spec

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestErrorBudget(t *testing.T) {
	cases := []struct {
		objective float64
		want      float64
	}{
		{99.9, 0.001},
		{99.0, 0.01},
		{99.99, 0.0001},
		{95.0, 0.05},
	}
	for _, c := range cases {
		s := SLO{Spec: Spec{Objective: c.objective}}
		got := s.ErrorBudget()
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("objective %g: error budget = %g, want %g", c.objective, got, c.want)
		}
	}
}

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"5m", 5 * time.Minute, false},
		{"1h", time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"4w", 4 * 7 * 24 * time.Hour, false},
		{"1h30m", 90 * time.Minute, false},
		{"", 0, true},
		{"banana", 0, true},
	}
	for _, c := range cases {
		got, err := ParseDuration(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseDuration(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDuration(%q): unexpected error %v", c.in, err)
			continue
		}
		if got.Std() != c.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", c.in, got.Std(), c.want)
		}
	}
}

func TestPrometheusRendering(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"5m", "5m"},
		{"1h", "1h"},
		{"24h", "1d"}, // largest whole unit preferred
		{"30d", "30d"},
		{"4w", "4w"},
		{"90m", "90m"},
	}
	for _, c := range cases {
		d, err := ParseDuration(c.in)
		if err != nil {
			t.Fatalf("ParseDuration(%q): %v", c.in, err)
		}
		if got := d.Prometheus(); got != c.want {
			t.Errorf("ParseDuration(%q).Prometheus() = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAlertBaseName(t *testing.T) {
	s := SLO{Metadata: Metadata{Name: "checkout-api-availability"}}
	if got, want := s.AlertBaseName(), "CheckoutApiAvailabilityErrorBudgetBurn"; got != want {
		t.Errorf("AlertBaseName() = %q, want %q", got, want)
	}
	s.Spec.Alerting.Name = "CustomBurn"
	if got, want := s.AlertBaseName(), "CustomBurn"; got != want {
		t.Errorf("AlertBaseName() with override = %q, want %q", got, want)
	}
}

func TestValidate(t *testing.T) {
	valid := func() SLO {
		return SLO{
			APIVersion: APIVersion,
			Metadata:   Metadata{Name: "checkout", Service: "checkout-api"},
			Spec: Spec{
				Objective: 99.9,
				Window:    "30d",
				Indicator: Indicator{
					Type:        IndicatorRatio,
					ErrorMetric: `http_requests_total{code=~"5.."}`,
					TotalMetric: `http_requests_total`,
				},
			},
		}
	}

	if err := valid().Validate(); err != nil {
		t.Fatalf("valid SLO rejected: %v", err)
	}

	mutations := map[string]func(*SLO){
		"bad apiVersion":   func(s *SLO) { s.APIVersion = "v2" },
		"missing name":     func(s *SLO) { s.Metadata.Name = "" },
		"missing service":  func(s *SLO) { s.Metadata.Service = "" },
		"objective 100":    func(s *SLO) { s.Spec.Objective = 100 },
		"objective 0":      func(s *SLO) { s.Spec.Objective = 0 },
		"bad window":       func(s *SLO) { s.Spec.Window = "soon" },
		"zero window":      func(s *SLO) { s.Spec.Window = "0" },
		"negative window":  func(s *SLO) { s.Spec.Window = "-1h" },
		"subminute window": func(s *SLO) { s.Spec.Window = "30s" },
		"bad indicator":    func(s *SLO) { s.Spec.Indicator.Type = "threshold" },
		"missing error":    func(s *SLO) { s.Spec.Indicator.ErrorMetric = "" },
		"missing total":    func(s *SLO) { s.Spec.Indicator.TotalMetric = "" },
	}
	for name, mutate := range mutations {
		s := valid()
		mutate(&s)
		if err := s.Validate(); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestLoadMultiDoc(t *testing.T) {
	const data = `
apiVersion: slo-autopilot/v1
metadata:
  name: checkout
  service: checkout-api
spec:
  objective: 99.9
  window: 30d
  indicator:
    type: ratio
    errorMetric: 'http_requests_total{code=~"5.."}'
    totalMetric: 'http_requests_total'
---
apiVersion: slo-autopilot/v1
metadata:
  name: search
  service: search-api
spec:
  objective: 99.5
  window: 7d
  indicator:
    type: ratio
    errorMetric: 'search_requests_total{result="error"}'
    totalMetric: 'search_requests_total'
`
	dir := t.TempDir()
	path := filepath.Join(dir, "slos.yaml")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	slos, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(slos) != 2 {
		t.Fatalf("loaded %d SLOs, want 2", len(slos))
	}
	if slos[0].Metadata.Name != "checkout" || slos[1].Metadata.Name != "search" {
		t.Errorf("unexpected names: %q, %q", slos[0].Metadata.Name, slos[1].Metadata.Name)
	}
}
