// Package gate turns a live error-budget Status into an allow/block decision
// for a deploy. This is the "error-budget policy as code" piece: when a service
// is out of budget or actively burning fast, new deploys are frozen until the
// service is healthy again — exactly the rule an SRE team would enforce by hand,
// made automatic and auditable in CI.
package gate

import (
	"fmt"

	"github.com/abhisheksoppanna/slo-autopilot/internal/budget"
)

// Policy configures what the gate considers blocking.
type Policy struct {
	// MinRemainingFraction blocks the deploy when the remaining error budget is
	// below this fraction (0.0–1.0). 0 means "only block when fully exhausted".
	MinRemainingFraction float64
	// BlockOnFastBurn blocks the deploy when a page-severity burn is active,
	// even if budget technically remains — you do not ship into an active fire.
	BlockOnFastBurn bool
}

// DefaultPolicy blocks only when the budget is fully exhausted or a page-level
// burn is active. It is intentionally permissive so adopting the gate does not
// immediately freeze every pipeline.
func DefaultPolicy() Policy {
	return Policy{MinRemainingFraction: 0, BlockOnFastBurn: true}
}

// Decision is the gate's verdict for one SLO.
type Decision struct {
	SLO     string
	Allowed bool
	Reasons []string
	Status  budget.Status
}

// Evaluate applies the policy to a budget Status.
func Evaluate(st budget.Status, p Policy) Decision {
	d := Decision{SLO: st.Name, Allowed: true, Status: st}

	if st.RemainingFraction <= p.MinRemainingFraction {
		d.Allowed = false
		if st.Exhausted() {
			d.Reasons = append(d.Reasons, fmt.Sprintf(
				"error budget exhausted (%.1f%% of budget consumed over %s)",
				st.ConsumedFraction*100, st.Name,
			))
		} else {
			d.Reasons = append(d.Reasons, fmt.Sprintf(
				"remaining error budget %.1f%% is below the required minimum %.1f%%",
				st.RemainingFraction*100, p.MinRemainingFraction*100,
			))
		}
	}

	if p.BlockOnFastBurn {
		if b, firing := st.FastestBurn(); firing {
			d.Allowed = false
			d.Reasons = append(d.Reasons, fmt.Sprintf(
				"active fast burn: %.1fx budget over the %s window (page severity)",
				b.BurnRate, b.Window.Long,
			))
		}
	}

	if d.Allowed {
		d.Reasons = append(d.Reasons, fmt.Sprintf(
			"healthy: %.1f%% of error budget remaining, no active page burn",
			st.RemainingFraction*100,
		))
	}
	return d
}

// EvaluateAll runs the gate across multiple SLOs. The overall result blocks if
// any single SLO blocks — a deploy touching several services is only as safe as
// its least healthy dependency.
func EvaluateAll(statuses []budget.Status, p Policy) (decisions []Decision, allowed bool) {
	allowed = true
	for _, st := range statuses {
		d := Evaluate(st, p)
		if !d.Allowed {
			allowed = false
		}
		decisions = append(decisions, d)
	}
	return decisions, allowed
}
