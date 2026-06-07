# SLO spec reference

One YAML file is the source of truth. Everything else — recording rules, alerts,
dashboards, the gate decision — is derived from it. A spec file may contain
multiple SLOs separated by `---`.

## Full example

```yaml
apiVersion: slo-autopilot/v1
metadata:
  name: checkout-api-availability   # unique slug; used in labels and rule names
  service: checkout-api             # the system being measured
  team: payments                    # routing key; copied onto every rule
  labels:                           # optional; propagated to all generated rules
    tier: critical
spec:
  description: 99.9% of checkout requests return a non-5xx response.
  objective: 99.9                   # target success %, in (0, 100)
  window: 30d                       # rolling compliance window
  indicator:
    type: ratio                     # only "ratio" is supported today
    errorMetric: 'http_requests_total{service="checkout-api", code=~"5.."}'
    totalMetric: 'http_requests_total{service="checkout-api"}'
  alerting:
    name: CheckoutErrorBudgetBurn   # optional; overrides the generated base name
```

## Fields

| Path | Required | Notes |
|------|----------|-------|
| `apiVersion` | yes | Must be `slo-autopilot/v1`. |
| `metadata.name` | yes | Unique slug. Becomes the `slo` label and the alert-name stem. |
| `metadata.service` | yes | The measured system; becomes the `service` label. |
| `metadata.team` | no | Added as a `team` label for routing. |
| `metadata.labels` | no | Extra labels on every generated rule. Cannot override `slo`/`service`/`team`. |
| `spec.description` | no | Shown in dashboards and alert annotations. |
| `spec.objective` | yes | Success target as a percentage in `(0, 100)`, e.g. `99.9`. Error budget = `1 - objective/100`. |
| `spec.window` | yes | Compliance window: `30d`, `4w`, `7d`, `1h`, … (Prometheus-style, plus `d`/`w`). |
| `spec.indicator.type` | yes | `ratio`. |
| `spec.indicator.errorMetric` | yes | Counter selector for **bad** events, **without** a range. |
| `spec.indicator.totalMetric` | yes | Counter selector for **all** events, **without** a range. |
| `spec.alerting.name` | no | Base alert name; severity + window are appended. |

## The metric selectors

`errorMetric` and `totalMetric` are **counter selectors with no range vector** —
write `http_requests_total{...}`, not `rate(http_requests_total{...}[5m])`. The
tool wraps each one in `rate(...[window])` for every burn-rate window itself, so:

- the SLI is computed identically in alerts, dashboards, and the gate; and
- adding or changing a window never means editing the spec.

The generated ratio for a window `W` is:

```
sum(rate(<errorMetric>[W])) / clamp_min(sum(rate(<totalMetric>[W])), 1e-9)
```

## Validation

`slo-autopilot validate -f <spec>` is strict on purpose — a spec that silently
generates nothing is worse than a loud failure in CI. It rejects a wrong
`apiVersion`, a missing name/service, an objective outside `(0,100)`, an
unparseable window, a non-`ratio` indicator, or empty metric selectors.

```console
$ slo-autopilot validate -f examples/checkout-api.slo.yaml
✓ 1 SLO(s) valid
  checkout-api-availability  checkout-api  objective 99.9% over 30d  (budget 0.1%)
```
