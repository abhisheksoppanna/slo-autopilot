// Command payment-api is a tiny sample service for the SLO Autopilot demo. It
// exposes a /checkout endpoint instrumented with RED metrics (Rate, Errors,
// Duration), a /chaos admin endpoint to inject failures and latency at runtime,
// and an optional self-load generator so metrics flow the moment it boots.
//
// It exists only to give Prometheus something realistic to scrape; it is not
// part of the slo-autopilot tool itself.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const serviceName = "payment-api"

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests, partitioned by path and response code.",
	}, []string{"service", "method", "path", "code"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	}, []string{"service", "path"})
)

// chaos holds runtime-tunable failure injection, updated atomically by /chaos.
type chaos struct {
	errorRate atomic.Uint64 // float64 bits, 0..1
	latencyNs atomic.Int64  // additional injected latency
}

func (c *chaos) setErrorRate(f float64) { c.errorRate.Store(math.Float64bits(clamp01(f))) }
func (c *chaos) getErrorRate() float64  { return math.Float64frombits(c.errorRate.Load()) }
func (c *chaos) setLatency(d time.Duration) {
	if d < 0 {
		d = 0
	}
	c.latencyNs.Store(int64(d))
}
func (c *chaos) getLatency() time.Duration { return time.Duration(c.latencyNs.Load()) }

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	selfLoad := flag.Bool("selfload", envBool("SELFLOAD", true), "generate internal traffic so metrics flow without an external client")
	rps := flag.Int("rps", envInt("RPS", 25), "self-load requests per second")
	initialErrors := flag.Float64("errors", envFloat("ERRORS", 0), "initial injected error rate (0..1)")
	healthcheck := flag.Bool("healthcheck", false, "probe /healthz and exit 0 (healthy) or 1 — used as the container HEALTHCHECK")
	flag.Parse()

	if *healthcheck {
		os.Exit(probeHealth(*addr))
	}

	c := &chaos{}
	c.setErrorRate(*initialErrors)

	mux := http.NewServeMux()
	mux.HandleFunc("/checkout", checkoutHandler(c))
	mux.HandleFunc("/chaos", chaosHandler(c))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{Addr: *addr, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if *selfLoad {
		go runSelfLoad(ctx, c, *rps)
	}

	go func() {
		log.Printf("payment-api listening on %s (selfload=%v rps=%d errors=%.2f)", *addr, *selfLoad, *rps, *initialErrors)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

// checkoutHandler simulates a checkout, recording RED metrics. Latency is a
// small base plus any injected latency; the response is a 5xx with probability
// equal to the current injected error rate.
func checkoutHandler(c *chaos) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code, dur := simulateCheckout(c)
		record(r.Method, "/checkout", code, dur)
		time.Sleep(dur)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(http.StatusText(code)))
	}
}

func simulateCheckout(c *chaos) (code int, dur time.Duration) {
	base := 8*time.Millisecond + time.Duration(rand.Int63n(int64(20*time.Millisecond)))
	dur = base + c.getLatency()
	if rand.Float64() < c.getErrorRate() {
		return http.StatusInternalServerError, dur
	}
	return http.StatusOK, dur
}

func record(method, path string, code int, dur time.Duration) {
	requestsTotal.WithLabelValues(serviceName, method, path, strconv.Itoa(code)).Inc()
	requestDuration.WithLabelValues(serviceName, path).Observe(dur.Seconds())
}

// chaosHandler reads/writes the injected error rate and latency at runtime, e.g.
//
//	curl 'http://localhost:8080/chaos?errors=0.5&latency=200ms'
//	curl 'http://localhost:8080/chaos?reset=1'
func chaosHandler(c *chaos) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("reset") != "" {
			c.setErrorRate(0)
			c.setLatency(0)
		}
		if v := q.Get("errors"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				c.setErrorRate(f)
			} else {
				http.Error(w, "errors must be a number in [0,1]", http.StatusBadRequest)
				return
			}
		}
		if v := q.Get("latency"); v != "" {
			if d, err := time.ParseDuration(v); err == nil {
				c.setLatency(d)
			} else {
				http.Error(w, "latency must be a duration like 200ms", http.StatusBadRequest)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errorRate": c.getErrorRate(),
			"latency":   c.getLatency().String(),
		})
	}
}

// runSelfLoad drives steady internal traffic so the demo dashboards populate
// immediately. It records metrics directly, exercising the same chaos logic as
// the HTTP path without the cost of real network round-trips.
func runSelfLoad(ctx context.Context, c *chaos, rps int) {
	if rps <= 0 {
		return
	}
	interval := time.Second / time.Duration(rps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			code, dur := simulateCheckout(c)
			record(http.MethodGet, "/checkout", code, dur)
		}
	}
}

// probeHealth performs a one-shot GET /healthz against the local server. It
// lets the container HEALTHCHECK reuse this binary instead of needing wget/curl
// in an otherwise minimal (distroless) image.
func probeHealth(addr string) int {
	host := addr
	if len(host) > 0 && host[0] == ':' {
		host = "localhost" + host
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + host + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// ---- small env helpers ----------------------------------------------------

func clamp01(f float64) float64 {
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	default:
		return f
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
