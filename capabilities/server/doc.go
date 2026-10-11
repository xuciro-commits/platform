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
// W3 consumer boundaries (ADR-0097): internal/host names the role each
// compiled owner receives; Host is only the composition aggregate. Console
// and Agents own their state and receive ConsoleHost/AgentHost, never Tenant.
// Construction and enterprise value contracts live in internal/host/buildapi
// and enterpriseapi; runtime code imports no compiled apps. Public DTOs retain
// the original host API schema names and journal encodings.
//
// Lock obligations: mu owns the tenant decision lane, installed definitions,
// agent-run capture and accepted-result application. opsMu protects queues,
// connectors, settings, notices and effects; mu may enter opsMu for a bounded
// capture/application, but opsMu must be released before acquiring mu. The
// source runner uses streamMu.TryLock before mu; the decision lane never waits
// for streamMu. Record, ledger, identity, file and projection owners retain
// their component locks. Already-held decision callbacks do not retake mu;
// Agent BeginAgentRead is reserved for outside-lane observations.
//
// Source/Compute/effect I/O captures fixed identity and versions under the
// decision lane, runs outside it, then revalidates authority and predecessor
// before acceptance. File handles keep digest/ticket/version bounds. Each
// owner keeps its original execution/recovery rules; there is no common I/O
// executor. Race tests do not prove freedom from deadlock.
//
// The pipeline is Tenant.Submit (host.go) → accepted_*.go (one file per result kind) →
// replay.go; snapshot.go carries every component's state. Routes live in
// routes.go and routes_*.go, one file per API area. Sub-packages: platform/
// (the apps' only API), apps/ (platform apps), journal/ (durable stores),
// idp/ (identity providers), internal/host (host-side interfaces).
package platformserver
