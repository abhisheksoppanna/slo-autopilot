// Package spec defines the SLO Autopilot specification format and its loader.
//
// An SLO is declared once, in YAML, and drives everything downstream:
// Prometheus recording + burn-rate alert rules, a Grafana dashboard, the live
// error-budget calculation, and the CI/CD release gate. Keeping a single source
// of truth is the whole point — humans edit the spec, the tool derives the rest.
package spec

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// APIVersion is the only spec version currently understood.
const APIVersion = "slo-autopilot/v1"

// SLO is a single service level objective.
type SLO struct {
	APIVersion string   `yaml:"apiVersion"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
}

// Metadata identifies the SLO and the thing it protects.
type Metadata struct {
	// Name uniquely identifies the SLO. Used as a Prometheus label and in
	// generated rule/alert names, so keep it short and slug-ish.
	Name string `yaml:"name"`
	// Service is the system the SLO measures (e.g. "checkout-api").
	Service string `yaml:"service"`
	// Team owns the SLO and is the default alert routing key.
	Team string `yaml:"team"`
	// Labels are propagated onto every generated rule and alert.
	Labels map[string]string `yaml:"labels,omitempty"`
}

// Spec is the measurable part of the objective.
type Spec struct {
	// Description is free text shown in dashboards and alert annotations.
	Description string `yaml:"description"`
	// Objective is the target success percentage over Window, e.g. 99.9.
	Objective float64 `yaml:"objective"`
	// Window is the rolling compliance window, e.g. "30d", "4w", "1h".
	Window string `yaml:"window"`
	// Indicator describes how to measure good vs. total events.
	Indicator Indicator `yaml:"indicator"`
	// Alerting overrides defaults for generated alerts (optional).
	Alerting Alerting `yaml:"alerting,omitempty"`
}

// Indicator is the SLI definition. Only the ratio method is supported today:
// errorMetric and totalMetric are counter selectors that the tool wraps in
// rate(...[window]) for each burn-rate window, so the SLI is measured
// identically everywhere it is referenced.
type Indicator struct {
	// Type must be "ratio".
	Type string `yaml:"type"`
	// ErrorMetric selects the counter of bad events, without a range, e.g.
	//   http_requests_total{service="checkout-api", code=~"5.."}
	ErrorMetric string `yaml:"errorMetric"`
	// TotalMetric selects the counter of all events, without a range, e.g.
	//   http_requests_total{service="checkout-api"}
	TotalMetric string `yaml:"totalMetric"`
}

// Alerting lets a spec override generated alert naming.
type Alerting struct {
	// Name is the base alert name; severities are suffixed (…Page, …Ticket).
	// Defaults to a PascalCase form of the SLO name + "ErrorBudgetBurn".
	Name string `yaml:"name,omitempty"`
}

// IndicatorType enumerates supported SLI methods.
const (
	IndicatorRatio = "ratio"
)

// ErrorBudget returns the allowed error fraction, e.g. 0.001 for a 99.9%
// objective. This is the denominator for every burn-rate calculation.
func (s SLO) ErrorBudget() float64 {
	return 1 - s.Spec.Objective/100
}

// WindowDuration parses the compliance window, supporting day (d) and week (w)
// units that Go's time package does not.
func (s SLO) WindowDuration() (Duration, error) {
	return ParseDuration(s.Spec.Window)
}

// AlertBaseName returns the configured alert base name, deriving a sensible
// default from the SLO name when none is set.
func (s SLO) AlertBaseName() string {
	if s.Spec.Alerting.Name != "" {
		return s.Spec.Alerting.Name
	}
	return pascal(s.Metadata.Name) + "ErrorBudgetBurn"
}

// RatioExpr returns the canonical PromQL error-ratio expression for the given
// window — sum(rate(error[w])) / clamp_min(sum(rate(total[w])), 1e-9). It is the
// single source of truth for how the SLI is measured, so the generated alerts,
// the dashboard, and the live gate all compute it identically. The clamp_min
// keeps an idle service (no traffic) reporting a 0 ratio rather than NaN.
func (i Indicator) RatioExpr(window Duration) string {
	w := window.Prometheus()
	return fmt.Sprintf("sum(rate(%s[%s])) / clamp_min(sum(rate(%s[%s])), 1e-9)",
		i.ErrorMetric, w, i.TotalMetric, w)
}

// Load reads one or more SLOs from a YAML file. Multi-document files
// (--- separated) are supported so a team can keep related SLOs together.
func Load(path string) ([]SLO, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open spec %q: %w", path, err)
	}
	defer f.Close()

	var slos []SLO
	dec := yaml.NewDecoder(f)
	for {
		var s SLO
		err := dec.Decode(&s)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parse spec %q: %w", path, err)
		}
		// Skip empty documents (e.g. a trailing ---).
		if s.APIVersion == "" && s.Metadata.Name == "" {
			continue
		}
		slos = append(slos, s)
	}
	if len(slos) == 0 {
		return nil, fmt.Errorf("spec %q contains no SLO documents", path)
	}
	for i := range slos {
		if err := slos[i].Validate(); err != nil {
			return nil, fmt.Errorf("spec %q (document %d): %w", path, i+1, err)
		}
	}
	return slos, nil
}

// Validate checks a single SLO for internal consistency. It is deliberately
// strict: a malformed SLO that silently generates no alerts is worse than a
// loud failure in CI.
func (s SLO) Validate() error {
	var problems []string

	if s.APIVersion != APIVersion {
		problems = append(problems, fmt.Sprintf("apiVersion must be %q, got %q", APIVersion, s.APIVersion))
	}
	if strings.TrimSpace(s.Metadata.Name) == "" {
		problems = append(problems, "metadata.name is required")
	}
	if strings.TrimSpace(s.Metadata.Service) == "" {
		problems = append(problems, "metadata.service is required")
	}
	if s.Spec.Objective <= 0 || s.Spec.Objective >= 100 {
		problems = append(problems, fmt.Sprintf("spec.objective must be in (0, 100), got %g", s.Spec.Objective))
	}
	if d, err := ParseDuration(s.Spec.Window); err != nil {
		problems = append(problems, fmt.Sprintf("spec.window %q is invalid: %v", s.Spec.Window, err))
	} else if d <= 0 {
		problems = append(problems, fmt.Sprintf("spec.window must be positive, got %q", s.Spec.Window))
	} else if d.Std() < time.Minute {
		problems = append(problems, fmt.Sprintf("spec.window must be at least 1m (Prometheus rate windows are coarse), got %q", s.Spec.Window))
	}
	if s.Spec.Indicator.Type != IndicatorRatio {
		problems = append(problems, fmt.Sprintf("spec.indicator.type must be %q, got %q", IndicatorRatio, s.Spec.Indicator.Type))
	}
	if strings.TrimSpace(s.Spec.Indicator.ErrorMetric) == "" {
		problems = append(problems, "spec.indicator.errorMetric is required")
	}
	if strings.TrimSpace(s.Spec.Indicator.TotalMetric) == "" {
		problems = append(problems, "spec.indicator.totalMetric is required")
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid SLO: %s", strings.Join(problems, "; "))
	}
	return nil
}

// pascal converts a slug like "checkout-api-availability" to "CheckoutApiAvailability".
func pascal(s string) string {
	var b strings.Builder
	upNext := true
	for _, r := range s {
		switch {
		case r == '-' || r == '_' || r == ' ' || r == '.':
			upNext = true
		case upNext:
			b.WriteString(strings.ToUpper(string(r)))
			upNext = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
