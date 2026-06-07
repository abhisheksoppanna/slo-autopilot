package dashboard

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/abhisheksoppanna/slo-autopilot/internal/burnrate"
	"github.com/abhisheksoppanna/slo-autopilot/internal/spec"
)

func sampleSLO() spec.SLO {
	return spec.SLO{
		APIVersion: spec.APIVersion,
		Metadata:   spec.Metadata{Name: "checkout-api-availability", Service: "checkout-api"},
		Spec: spec.Spec{
			Objective: 99.9,
			Window:    "30d",
			Indicator: spec.Indicator{
				Type:        spec.IndicatorRatio,
				ErrorMetric: `http_requests_total{code=~"5.."}`,
				TotalMetric: `http_requests_total`,
			},
		},
	}
}

func TestGenerateValidJSON(t *testing.T) {
	out, err := Generate(sampleSLO(), burnrate.Standard())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var dash map[string]any
	if err := json.Unmarshal(out, &dash); err != nil {
		t.Fatalf("dashboard is not valid JSON: %v", err)
	}
	if dash["title"] == nil {
		t.Error("dashboard missing title")
	}
	panels, ok := dash["panels"].([]any)
	if !ok || len(panels) == 0 {
		t.Fatalf("dashboard has no panels")
	}
	// Every panel must bind to the provisioned datasource UID, or it renders blank.
	if !strings.Contains(string(out), `"uid": "`+DatasourceUID+`"`) {
		t.Error("panels do not reference the provisioned datasource UID")
	}
}

func TestDashboardUIDBounded(t *testing.T) {
	s := sampleSLO()
	s.Metadata.Name = strings.Repeat("very-long-slo-name-", 5)
	out, err := Generate(s, burnrate.Standard())
	if err != nil {
		t.Fatal(err)
	}
	var dash map[string]any
	if err := json.Unmarshal(out, &dash); err != nil {
		t.Fatal(err)
	}
	if uid, _ := dash["uid"].(string); len(uid) > 40 {
		t.Errorf("dashboard UID %q exceeds Grafana's 40-char limit", uid)
	}
}
