// Package platformserver is the host: it composes a tenant's apps, takes
// members' submissions through the accepted-result pipeline, runs the owned
// work and effects they cause, and serves the tenant API (ADR-0080 layout).
//
// A Tenant (host.go) is an identity, the submit pipeline, and holders for
// components that each own their state and, where reads cross the pipeline,
// their own lock:
//
//	committed        committed.go          answers by idempotency key
//	audit            audit_log.go          recent inputs, deliveries, personal reads
//	releases         release_store.go      candidates, applied digests, active release
//	console          host_control.go       lifecycle, support sessions, migrations
//	staged           compute_channel.go    the per-call result channel
//	work             work_board.go         queues, failed deliveries, jobs
//	quota            quota.go              attempts per app per minute
//	connectors       connector_roster.go   registry, descriptor index, last refusals
//	notices          notices.go            notifications
//	settings         settings.go           app settings
//	sequences        sequences.go          number counters
//	uploads          uploads.go            file hashes not yet attached
//	cancels          compute_cancel_registry.go
//	i18n             translator.go         dictionaries and patterns
//	memStore         journal/memory.go     the Store without one
//
// The pipeline is Tenant.Submit (host.go) → accepted_*.go (one file per result kind) →
// replay.go; snapshot.go carries every component's state. Routes live in
// routes.go and routes_*.go, one file per API area. Sub-packages: platform/
// (the apps' only API), apps/ (platform apps), journal/ (durable stores),
// idp/ (identity providers), internal/host (host-side interfaces).
package platformserver
