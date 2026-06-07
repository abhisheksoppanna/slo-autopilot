// Package dashboard generates a Grafana dashboard (JSON model) for an SLO. The
// panels visualise the error budget the same way the gate reasons about it:
// budget remaining, burn rate as a multiple of the budget, the SLI against its
// objective, and whether any page-severity alert is firing.
//
// Panels reference the recording rules emitted by the promrules package, so the
// dashboard and the alerts always agree on how the SLI is measured.
package dashboard

import (
	"encoding/json"
	"fmt"

	"github.com/abhisheksoppanna/slo-autopilot/internal/burnrate"
	"github.com/abhisheksoppanna/slo-autopilot/internal/promrules"
	"github.com/abhisheksoppanna/slo-autopilot/internal/spec"
)

// DatasourceUID is the Prometheus datasource UID the panels bind to. The demo
// provisions Grafana with a datasource of this exact UID.
const DatasourceUID = "prometheus"

type obj = map[string]any

// Generate builds a Grafana dashboard JSON model for the SLO under the policy.
func Generate(s spec.SLO, p burnrate.Policy) ([]byte, error) {
	sel := fmt.Sprintf(`{slo="%s"}`, s.Metadata.Name)
	budget := s.ErrorBudget()

	var panels []obj
	id := 0
	next := func() int { id++; return id }

	// Row 0: headline stats.
	budgetRemainingExpr := fmt.Sprintf("clamp_min(1 - (%s) / %s, 0) * 100",
		fullWindowRatioExpr(s), gFloat(budget))
	panels = append(panels, statPanel(next(), "Error budget remaining",
		budgetRemainingExpr, gridPos(0, 0, 8, 8), "percent",
		[]threshold{{nil, "red"}, {f(10), "orange"}, {f(25), "green"}},
	))

	panels = append(panels, statPanel(next(), "Current burn rate",
		fmt.Sprintf("%s%s / %s", promrules.RecordingRuleName(fastest(p)), sel, gFloat(budget)),
		gridPos(8, 0, 8, 8), "none",
		[]threshold{{nil, "green"}, {f(1), "orange"}, {f(14.4), "red"}},
	))

	panels = append(panels, statPanel(next(), "Page alerts firing",
		fmt.Sprintf(`sum(ALERTS{slo="%s",severity="page",alertstate="firing"}) or vector(0)`, s.Metadata.Name),
		gridPos(16, 0, 8, 8), "none",
		[]threshold{{nil, "green"}, {f(1), "red"}},
	))

	// Row 1: SLI vs objective over time.
	sliExpr := fmt.Sprintf("(1 - %s%s) * 100", promrules.RecordingRuleName(shortest(p)), sel)
	sli := timeseriesPanel(next(), "SLI — success ratio vs objective",
		[]target{
			{Expr: sliExpr, Legend: "success %"},
			{Expr: gFloat(s.Spec.Objective), Legend: "objective"},
		},
		gridPos(0, 8, 24, 8), "percent",
	)
	panels = append(panels, sli)

	// Row 2: burn rate per window over time, with the page threshold marked.
	var burnTargets []target
	for _, w := range p.Windows {
		burnTargets = append(burnTargets, target{
			Expr:   fmt.Sprintf("%s%s / %s", promrules.RecordingRuleName(w.Long), sel, gFloat(budget)),
			Legend: fmt.Sprintf("%s burn (%gx threshold)", w.Long, w.Factor),
		})
	}
	burn := timeseriesPanel(next(), "Error-budget burn rate (multiples of budget)",
		burnTargets, gridPos(0, 16, 24, 9), "none")
	panels = append(panels, burn)

	dash := obj{
		"uid":           dashUID(s.Metadata.Name),
		"title":         fmt.Sprintf("SLO — %s", s.Metadata.Name),
		"description":   s.Spec.Description,
		"tags":          []string{"slo-autopilot", s.Metadata.Service},
		"schemaVersion": 39,
		"version":       1,
		"editable":      true,
		"refresh":       "10s",
		"time":          obj{"from": "now-1h", "to": "now"},
		"timezone":      "browser",
		"annotations":   obj{"list": []any{}},
		"templating":    obj{"list": []any{}},
		"panels":        panels,
	}

	return json.MarshalIndent(dash, "", "  ")
}

// fullWindowRatioExpr is the inline error ratio over the full compliance window,
// used for the budget-remaining stat (the compliance window is not one of the
// policy's recording-rule windows).
func fullWindowRatioExpr(s spec.SLO) string {
	w, err := s.WindowDuration()
	win := "30d"
	if err == nil {
		win = w.Prometheus()
	}
	return fmt.Sprintf("sum(rate(%s[%s])) / clamp_min(sum(rate(%s[%s])), 1e-9)",
		s.Spec.Indicator.ErrorMetric, win, s.Spec.Indicator.TotalMetric, win)
}

func fastest(p burnrate.Policy) spec.Duration {
	// Shortest long-window page alert = the fastest burn signal.
	best := p.Windows[0].Long
	for _, w := range p.Windows {
		if w.Long < best {
			best = w.Long
		}
	}
	return best
}

func shortest(p burnrate.Policy) spec.Duration {
	ds := p.DistinctWindows()
	return ds[0]
}

// ---- panel builders -------------------------------------------------------

type target struct {
	Expr   string
	Legend string
}

type threshold struct {
	Value *float64 // nil = base
	Color string
}

func statPanel(id int, title, expr string, gp obj, unit string, steps []threshold) obj {
	return obj{
		"id":         id,
		"type":       "stat",
		"title":      title,
		"datasource": datasource(),
		"gridPos":    gp,
		"targets":    []obj{{"expr": expr, "refId": "A", "datasource": datasource()}},
		"fieldConfig": obj{
			"defaults": obj{
				"unit":       unit,
				"thresholds": obj{"mode": "absolute", "steps": thresholdSteps(steps)},
				"color":      obj{"mode": "thresholds"},
			},
			"overrides": []any{},
		},
		"options": obj{
			"colorMode":   "background",
			"graphMode":   "area",
			"justifyMode": "auto",
			"textMode":    "value_and_name",
			"reduceOptions": obj{
				"calcs":  []string{"lastNotNull"},
				"fields": "",
				"values": false,
			},
		},
	}
}

func timeseriesPanel(id int, title string, targets []target, gp obj, unit string) obj {
	var ts []obj
	for i, t := range targets {
		ts = append(ts, obj{
			"expr":         t.Expr,
			"legendFormat": t.Legend,
			"refId":        string(rune('A' + i)),
			"datasource":   datasource(),
		})
	}
	return obj{
		"id":         id,
		"type":       "timeseries",
		"title":      title,
		"datasource": datasource(),
		"gridPos":    gp,
		"targets":    ts,
		"fieldConfig": obj{
			"defaults": obj{
				"unit": unit,
				"custom": obj{
					"drawStyle":         "line",
					"lineWidth":         2,
					"fillOpacity":       8,
					"showPoints":        "never",
					"spanNulls":         true,
					"axisPlacement":     "auto",
					"gradientMode":      "opacity",
					"scaleDistribution": obj{"type": "linear"},
				},
			},
			"overrides": []any{},
		},
		"options": obj{
			"legend":  obj{"displayMode": "table", "placement": "bottom", "calcs": []string{"lastNotNull", "max"}},
			"tooltip": obj{"mode": "multi", "sort": "desc"},
		},
	}
}

func thresholdSteps(steps []threshold) []obj {
	out := make([]obj, 0, len(steps))
	for _, s := range steps {
		var v any
		if s.Value == nil {
			v = nil
		} else {
			v = *s.Value
		}
		out = append(out, obj{"value": v, "color": s.Color})
	}
	return out
}

func datasource() obj { return obj{"type": "prometheus", "uid": DatasourceUID} }

func gridPos(x, y, w, h int) obj {
	return obj{"x": x, "y": y, "w": w, "h": h}
}

func dashUID(name string) string {
	const maxUID = 40
	uid := "slo-" + name
	if len(uid) > maxUID {
		uid = uid[:maxUID]
	}
	return uid
}

func f(v float64) *float64 { return &v }

// gFloat formats a float compactly for embedding in a PromQL expression.
func gFloat(v float64) string { return fmt.Sprintf("%g", v) }
