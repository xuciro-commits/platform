// Package platformserver is the host: it composes a tenant's apps, takes
// members' submissions through the accepted-result pipeline, runs the owned
// work and effects they cause, and serves the tenant API (ADR-0064 layout).
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
//	connectors       connector_roster.go   registry, descriptor index, last refusals
//	notices          notices.go            notifications
//	settings         internal/tenantstate  app settings
//	sequences        internal/tenantstate  number counters
//	uploads          internal/tenantstate  unattached file hashes
//	cancels          internal/tenantstate  running cancellation handles
//	quota            internal/tenantstate  attempt pacing and fair turn
//	i18n             translator.go         dictionaries and patterns
//	memStore         journal/memory.go     the Store without one
//
// W3 consumer boundaries (ADR-0037): internal/host names the role each
// compiled owner receives; Host is only the composition aggregate. Console
// and Agents own their state and receive ConsoleHost/AgentHost, never Tenant.
// Pure construction and enterprise declarations live in internal/construction
// and internal/enterprise; UAF runtime data is generated before the build.
// Record images, immutable history segments, recent changes, indexes and pure
// field/scope/query compilation live in internal/records. Host recordStore owns authorized
// storage access, private decision coordination and accepted application. SQL
// Rows use a lane-owned repeatable-read source and immutable private overlays;
// accepted application clears overlays before admitting the next read view.
// SQL owner checkpoints restore through their fixed archive plus accepted tail;
// portable snapshots and legacy reconstruction keep their original obligations.
// Construction owns pure object/process compilation and declaration/member
// projections; original owners supply narrow read roles and execute results.
// Accepted canonical encoding and strict decoding live in internal/acceptance.
// Seven pure result families, shared evidence and the typed change plan live
// in internal/acceptance/result. Runtime-owned Task/Agent/console formats keep
// their owner-specific checks and use those same shared values and decoders.
// Agent prompt/history compilation, observation bounds and budget arithmetic
// live in internal/agents, which receives only already-authorized values.
// internal/aistate owns the unchanged AI state format and pure usage updates;
// the AI app keeps live reservations while SQL consumes the same saved rules.
// Runtime code imports no compiled apps. Public DTOs retain
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
// The pipeline is Tenant.Submit (host.go) → accepted_*.go (one file per result kind) →
// replay.go; snapshot.go carries every component's state. Routes live in
// routes.go and routes_*.go, one file per API area. Sub-packages: platform/
// (the apps' only API), apps/ (platform apps), journal/ (durable stores),
// idp/ (identity providers), internal/host (host-side interfaces).
package platformserver
