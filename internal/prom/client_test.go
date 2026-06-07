package prom

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serverReturning(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

// These cases pin the exact contract budget.go depends on: empty vector ->
// ErrNoData (treated as an idle service), NaN -> 0, Inf/status-error/unsupported
// result types -> error, and scalar vs vector reduction.
func TestQueryScalar(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		want       float64
		wantErr    bool
		wantNoData bool
	}{
		{name: "scalar", body: `{"status":"success","data":{"resultType":"scalar","result":[1700000000,"0.25"]}}`, want: 0.25},
		{name: "vector single", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1700000000,"0.5"]}]}}`, want: 0.5},
		{name: "empty vector", body: `{"status":"success","data":{"resultType":"vector","result":[]}}`, wantNoData: true},
		{name: "NaN to zero", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1700000000,"NaN"]}]}}`, want: 0},
		{name: "Inf errors", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1700000000,"+Inf"]}]}}`, wantErr: true},
		{name: "status error", body: `{"status":"error","errorType":"bad_data","error":"boom"}`, wantErr: true},
		{name: "matrix unsupported", body: `{"status":"success","data":{"resultType":"matrix","result":[]}}`, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := serverReturning(c.body)
			defer srv.Close()

			got, err := New(srv.URL).QueryScalar(context.Background(), "up")
			switch {
			case c.wantNoData:
				if !errors.Is(err, ErrNoData) {
					t.Fatalf("want ErrNoData, got err=%v val=%v", err, got)
				}
			case c.wantErr:
				if err == nil {
					t.Fatalf("want error, got nil (val %v)", got)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got != c.want {
					t.Errorf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}

func TestQueryScalarBadURL(t *testing.T) {
	// A connection failure must surface as an error, not a silent zero.
	_, err := New("http://127.0.0.1:0").QueryScalar(context.Background(), "up")
	if err == nil {
		t.Fatal("expected an error querying an unreachable Prometheus")
	}
}
