# Multi-window, multi-burn-rate alerting

This is the method SLO Autopilot generates. It comes from the
[Google SRE Workbook, "Alerting on SLOs"](https://sre.google/workbook/alerting-on-slos/).
The short version of *why* — and exactly what the tool emits — is below.

## The problem with naive alerting

Say your SLO is **99.9% success over 30 days**. That is an error budget of
**0.1%** of requests. The obvious alert is "page me when the error rate is above
0.1%." It fails in both directions:

- **Too slow at the top end.** A 100% outage at 0.1% threshold still has to
  *average* above 0.1% over the alert window before it fires. With a long
  averaging window you burn a huge chunk of budget before the page.
- **Too noisy at the bottom end.** A short window makes a brief 0.5% blip page
  you at 3 a.m. for something that consumed a rounding error of budget.

You cannot fix this with a single threshold and a single window.

## Burn rate

**Burn rate** normalises the error rate against the budget:

```
burn_rate = error_ratio / error_budget
```

- burn rate **1** → you will consume exactly 100% of the budget over the full
  compliance window. Sustainable, by definition.
- burn rate **14.4** → you will consume 100% of a 30-day budget in
  `30d / 14.4 ≈ 2 days`. Two hours of that burns 2% of the month. Page now.

Alerting on burn rate instead of raw error rate makes the threshold
*objective-relative*: the same policy works for a 99% and a 99.99% SLO.

## Two windows per alert

Each alert pairs a **long** window (the signal) with a **short** window (the
reset). The alert fires only when burn rate is high over **both**:

```
burn_rate(long)  > factor   AND   burn_rate(short) > factor
```

- The long window gives a stable, low-noise signal.
- The short window makes the alert **stop** quickly once the burn ends, instead
  of smouldering for the length of the long window.

## The policy SLO Autopilot generates

The `standard` policy (tuned for a 30-day window, straight from the workbook):

| Severity | Long | Short | Factor | Budget burned if sustained |
|----------|------|-------|--------|----------------------------|
| page     | 1h   | 5m    | 14.4   | 2% over 1h                 |
| page     | 6h   | 30m   | 6      | 5% over 6h                 |
| ticket   | 1d   | 2h    | 3      | 10% over 1d                |
| ticket   | 3d   | 6h    | 1      | 10% over 3d                |

The two **page** rows catch fast, dangerous burns. The two **ticket** rows catch
slow leaks that would still blow the budget by month-end but do not warrant
waking anyone.

The `fast` policy is the same shape compressed ~12× (5m/15m/1h/3h long windows)
so the local demo produces a full burn — and a blocked deploy — in minutes
instead of days. **Never run `fast` in production.**

## What this looks like in Prometheus

For each distinct window the tool emits one recording rule:

```yaml
- record: slo:sli_error:ratio_rate1h
  expr: |
    sum(rate(http_requests_total{service="checkout-api",code=~"5.."}[1h]))
    /
    clamp_min(sum(rate(http_requests_total{service="checkout-api"}[1h])), 1e-9)
  labels: { slo: checkout-api-availability, service: checkout-api }
```

(`clamp_min` keeps an idle service reporting a 0 ratio instead of `NaN`.)

Then each alert references the recording rules, so the expression stays cheap and
the SLI is measured identically everywhere:

```yaml
- alert: CheckoutApiAvailabilityErrorBudgetBurnPage1h
  expr: |
    (
      slo:sli_error:ratio_rate1h{slo="checkout-api-availability"} > 0.0144
      and
      slo:sli_error:ratio_rate5m{slo="checkout-api-availability"} > 0.0144
    )
  for: 2m
  labels: { severity: page, burn_window: 1h }
```

`0.0144 = 14.4 × 0.001` — the page factor times the error budget.

## Why the gate uses the same numbers

The release gate (`slo-autopilot gate`) does not invent a second definition of
"unhealthy." It reads the same burn rate over the same windows and blocks a
deploy when a **page-severity** window is firing, or when the budget for the full
window is already spent. The thing that pages you and the thing that freezes your
deploy are, deliberately, the same thing.
