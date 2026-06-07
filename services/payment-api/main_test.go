package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClamp01(t *testing.T) {
	cases := map[float64]float64{-1: 0, 0: 0, 0.37: 0.37, 1: 1, 2: 1}
	for in, want := range cases {
		if got := clamp01(in); got != want {
			t.Errorf("clamp01(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestChaosRoundTrip(t *testing.T) {
	c := &chaos{}
	c.setErrorRate(0.37)
	if got := c.getErrorRate(); got != 0.37 {
		t.Errorf("errorRate round-trip = %v, want 0.37", got)
	}
	c.setErrorRate(5) // out of range -> clamps to 1
	if got := c.getErrorRate(); got != 1 {
		t.Errorf("errorRate clamp = %v, want 1", got)
	}
	c.setLatency(150 * time.Millisecond)
	if got := c.getLatency(); got != 150*time.Millisecond {
		t.Errorf("latency round-trip = %v, want 150ms", got)
	}
}

func TestSimulateCheckoutDeterministicAtExtremes(t *testing.T) {
	always := &chaos{}
	always.setErrorRate(1)
	for i := 0; i < 200; i++ {
		if code, _ := simulateCheckout(always); code != http.StatusInternalServerError {
			t.Fatalf("errorRate 1.0 must always 500, got %d", code)
		}
	}
	never := &chaos{}
	never.setErrorRate(0)
	for i := 0; i < 200; i++ {
		if code, _ := simulateCheckout(never); code != http.StatusOK {
			t.Fatalf("errorRate 0 must always 200, got %d", code)
		}
	}
}

func TestChaosHandler(t *testing.T) {
	c := &chaos{}
	h := chaosHandler(c)

	// Non-numeric error rate is rejected.
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/chaos?errors=abc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("errors=abc -> %d, want 400", rec.Code)
	}

	// Valid params are applied and acknowledged.
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/chaos?errors=0.5&latency=200ms", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("valid chaos -> %d, want 200", rec.Code)
	}
	if c.getErrorRate() != 0.5 {
		t.Errorf("errorRate not applied: got %v", c.getErrorRate())
	}
	if c.getLatency() != 200*time.Millisecond {
		t.Errorf("latency not applied: got %v", c.getLatency())
	}

	// reset clears injected chaos.
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/chaos?reset=1", nil))
	if c.getErrorRate() != 0 || c.getLatency() != 0 {
		t.Errorf("reset did not clear chaos: errors=%v latency=%v", c.getErrorRate(), c.getLatency())
	}
}
