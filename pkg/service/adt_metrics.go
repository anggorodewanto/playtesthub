package service

import "github.com/prometheus/client_golang/prometheus"

// ADTLinkFailures counts ADT linking-flow failures by phase + reason
// so a Prometheus alert can fire when first-customer linking attempts
// regress. Counter, not Gauge — every failure is a discrete event,
// and bursts of the same reason are what we want to see.
//
// Labels:
//
//	phase  — "start" (StartADTLink) or "complete" (CompleteADTLink)
//	reason — narrow vocabulary so the alert rule stays readable:
//	         "config_missing"    — ADT_BASE_URL / ADT_REDIRECT_BASE_URL absent at StartADTLink time
//	         "studio_unresolved" — service token carries no usable namespace claim
//	         "store_error"       — Postgres write/read failure
//	         "state_invalid"     — state nonce missing, expired, or already consumed
//	         "adt_namespace_missing" — callback URL lacked adt_namespace
//	         "audit_failed"      — linkage inserted but the audit-row append failed (soft)
//
// Registered on the served *prometheus.Registry in main.go via
// RegisterADTMetrics so the metric appears on /metrics. Tests do not
// register and skip the counter (Inc on an unregistered counter is a
// no-op once registered, but pre-registration the var is nil-safe via
// the noop sentinel pattern below).
var ADTLinkFailures = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "playtesthub",
		Subsystem: "adt",
		Name:      "link_failures_total",
		Help:      "ADT linking-flow failures partitioned by phase and reason. See pkg/service/adt_metrics.go for the reason vocabulary.",
	},
	[]string{"phase", "reason"},
)

// ADTUnlinkADTSideFailures counts UnlinkADT's best-effort ADT-side
// DELETE failures so an alert can fire when a real ADT outage starts
// stranding operators (today's 2026-05-21 bug — an unlinked local row
// + a still-flagged ADT side blocks every subsequent re-link with 409
// `already_linked`). UnlinkADT swallows the error and finishes the
// local soft-delete; the metric is the only signal that the orphan
// case is accumulating.
//
// Labels:
//
//	reason — narrow vocabulary mirroring ADTLinkFailures:
//	         "linkage_missing" — ADT returned 401/403 (flag already absent on ADT's side; benign)
//	         "transient"       — ErrUnavailable / ErrRateLimited (5xx-retry exhausted or 429)
//	         "unknown"         — any other error class (logged + counted but otherwise opaque)
var ADTUnlinkADTSideFailures = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "playtesthub",
		Subsystem: "adt",
		Name:      "unlink_adt_side_failures_total",
		Help:      "UnlinkADT best-effort ADT-side DELETE failures partitioned by reason. See pkg/service/adt_metrics.go for the reason vocabulary.",
	},
	[]string{"reason"},
)

// RegisterADTMetrics attaches the ADT-linking counters to the supplied
// Prometheus registry. Idempotent within a process — calling twice
// against the same registry panics, so main.go calls it exactly once
// alongside the other collector registrations.
func RegisterADTMetrics(r prometheus.Registerer) {
	r.MustRegister(ADTLinkFailures)
	r.MustRegister(ADTUnlinkADTSideFailures)
}
