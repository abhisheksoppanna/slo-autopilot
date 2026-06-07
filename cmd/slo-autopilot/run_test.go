package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const sampleSpec = `apiVersion: slo-autopilot/v1
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
`

func writeSpec(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "slo.yaml")
	if err := os.WriteFile(p, []byte(sampleSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// promServer is a fake Prometheus reporting a fixed error ratio for every query.
func promServer(t *testing.T, ratio string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1700000000,"` + ratio + `"]}]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// The exit-code contract is the whole point of the gate: 0 = ship, 1 = frozen by
// policy, 2 = misuse. A CI pipeline keys off these, so pin them directly.
func TestRunExitCodes(t *testing.T) {
	specPath := writeSpec(t)

	t.Run("gate healthy -> 0", func(t *testing.T) {
		url := promServer(t, "0.0001") // 10% of the 0.1% budget consumed
		if code := run([]string{"gate", "-f", specPath, "--prometheus", url}); code != 0 {
			t.Errorf("healthy gate exit = %d, want 0", code)
		}
	})

	t.Run("gate burning -> 1", func(t *testing.T) {
		url := promServer(t, "0.05") // 50x budget: exhausted + active page burn
		if code := run([]string{"gate", "-f", specPath, "--prometheus", url}); code != 1 {
			t.Errorf("burning gate exit = %d, want 1", code)
		}
	})

	t.Run("misuse -> 2", func(t *testing.T) {
		if code := run([]string{"gate"}); code != 2 { // missing -f
			t.Errorf("missing-spec exit = %d, want 2", code)
		}
		if code := run([]string{"bogus-command"}); code != 2 {
			t.Errorf("unknown command exit = %d, want 2", code)
		}
		if code := run(nil); code != 2 {
			t.Errorf("no-args exit = %d, want 2", code)
		}
	})

	t.Run("validate ok -> 0", func(t *testing.T) {
		if code := run([]string{"validate", "-f", specPath}); code != 0 {
			t.Errorf("validate exit = %d, want 0", code)
		}
	})

	t.Run("version -> 0", func(t *testing.T) {
		if code := run([]string{"version"}); code != 0 {
			t.Errorf("version exit = %d, want 0", code)
		}
	})
}
