// Package platformserver is the host: it composes a tenant's apps, takes
// members' submissions through the accepted-result pipeline, runs the owned
// work and effects they cause, and serves the tenant API.
//
// A Tenant (host.go) is an identity, the submit pipeline, and holders for
// components that each own their state and, where reads cross the pipeline,
// their own lock:
//
//	committed        kernel/tenant_state.go answers by idempotency key
//	audit            kernel/tenant_diagnostics.go recent inputs, deliveries, personal reads
//	releases         studio/releases/release_retention.go candidates, applied digests, active release
//	console          kernel/control/control_host.go       lifecycle, support sessions, migrations
//	staged           engine/compute/compute_execution.go    the per-call result channel
//	work             engine/automation/automation_work.go         queues, failed deliveries, jobs
//	quota            engine/automation/automation_work.go              attempts per app per minute
//	connectors       connector_roster.go   registry, descriptor index, last refusals
//	notices          kernel/tenant_notifications.go notifications
//	settings         kernel/control/control_settings.go app settings
//	sequences        kernel/tenant_state.go          number counters
//	uploads          engine/resources/resource_files.go            file hashes not yet attached
//	cancels          engine/compute/compute_lifecycle.go
//	i18n             kernel/transport/transport_declarations.go         dictionaries and patterns
//	memStore         journal/memory.go     the Store without one
//
// Consumer boundaries are named public roles in platform. composition binds
// actual owners for startup and the same private decision/trial factories.
// Agent and Knowledge runtime algorithms live in engine; author evaluation and
// Build compilation live in studio. They receive their declared roles, never
// Tenant. Credentials and settings keep their original state in kernel/control.
// Record images, immutable history segments, recent changes, indexes and pure
// field/scope/query compilation and Store live in kernel/records. Host adapters
// own authorized reads, private decision coordination and accepted application. SQL
// Rows use a lane-owned repeatable-read source and immutable private overlays;
// accepted application clears overlays before admitting the next read view.
// SQL owner checkpoints restore through their fixed archive plus accepted tail;
// portable snapshots and legacy reconstruction keep their original obligations.
// studio/build owns object/process compilation and declaration/member
// projections; original owners supply narrow read roles and execute results.
// Accepted versioned encoding, strict decoding, shared evidence and the typed
// change plan, including original Task/Agent/console result envelopes, live in
// kernel/acceptedformat. Runtime owners keep their execution/application checks;
// kernel/tenant_recovery.go maps saved results to SQL deltas and checkpoints.
// Agent prompt/history compilation, observation bounds and budget arithmetic
// live in internal/agents, which receives only already-authorized values.
// internal/aistate owns the unchanged AI state format and pure usage updates;
// the AI app keeps live reservations while SQL consumes the same saved rules.
// Runtime owners use public roles; host installation adapters retain Build
// compilation dependencies pending the remaining boundary work. Public DTOs retain
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
// executor. Confirmation/Dispatch share disposable status-read scheduling and
// captured-access checks; their evidence and resend policies stay distinct.
// Race tests do not prove freedom from deadlock.
//
// Package direction rule (B3): tools (cmd/ one-shot binaries and scripts/)
// may import domains; a domain file group owns its result kinds and state and
// imports only platform/, internal/* and journal/ — never another domain's
// private files; the host shell (host.go) composes the domains and is the
// only place allowed to reach across them. routes_*.go are the shell's API
// surface: they call domains, and domains never call routes.
//
// The pipeline is Tenant.Submit (submit.go) → acceptance_pipeline.go /
// acceptance_records.go / acceptance_codec.go → replay.go; snapshot.go carries
// every component's state.
// Reads and connector inputs live in reads.go, accepted-event planning and
// delivery in events.go, and the member-facing facade (catalog, declarations,
// apps, directory) in facade.go. Routes live in routes.go and routes_*.go,
// one file per API area. Sub-packages: platform/ (the apps' only API), apps/
// (platform apps), journal/ (durable stores), idp/ (identity providers),
// engine/ and studio/ (runtime and authoring owners).
package platformserver
