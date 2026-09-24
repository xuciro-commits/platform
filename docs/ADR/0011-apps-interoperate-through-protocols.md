# ADR-0011: Apps interoperate through protocols; bridges are the exception

**Status:** Accepted (2026-09-24, owner direction; implemented in #95). Supersedes ADR-0009's bridges as the default; amends ADR-0010 point 3.

**Context.** ADR-0009 joined CRM and Hotel with a bridge package: one app per pair of apps. The owner observed that large software does not interoperate that way:
- VS Code works with GitHub through its extension and authentication-provider interfaces.
- GitHub accepts Google sign-in through OIDC.
- Claude Code uses Figma through MCP, and any MCP client can use Figma.

Interoperation comes from protocols that any app can implement or consume, not from pairwise bridges. With n apps, bridges grow toward n² and each needs its own grants, journal entries and upkeep. The platform already has most of a protocol:
- declared actions with descriptions, like MCP tools;
- reads, like MCP resources;
- events;
- routing through the host;
- grants.

What is missing is a way to depend on *a capability* rather than on *an app*.

**Decision.**

1. **Protocols.** A protocol is a named, versioned interface. It consists of:
   - actions (payload fields and meaning);
   - reads (result shape);
   - events.

   Example: `lodging.booking/1`, with the action reserve, the read bookings, and the events changed and canceled. Protocols are declared in typed code with their own conformance tests, the way the kernel contract has vectors.
2. **Apps provide and consume protocols.**
   - A manifest says which protocols the app **provides**, each mapped onto its own actions, reads and events.
   - A manifest says which protocols the app **consumes**, marking each as required or optional.
   - Requirements name protocols, not apps. When the tenant starts, the host binds each consumed protocol to an enabled provider, and discovery lists providers by protocol.
   - A tenant with two providers of one protocol chooses in Settings (#99): the choice is a decision of the platform app and decides where new calls go. Reads span every provider, each answer carrying the type of the entities it holds, so what the other provider holds stays visible and its events still reach linked entities.
3. **Shared relations are platform capabilities, not bridge data.**
   - **Links:** typed references between any two entities (K1 references, like Salesforce related records or Notion relations). An opportunity links to a booking without either app owning the link.
   - **Timeline:** notes and activities about any entity, posted by people or by apps through events.

   These two capabilities replace what the crm-hotel bridge owned.
4. **Consumers act through the protocol with the caller's grants.**
   - A consumer calls the provider's mapped action.
   - The provider's roles and rules decide the call (ADR-0008 unchanged).
   - Events of a protocol reach every consumer that subscribed to it.
5. **Where protocols come from.**
   - **Cross-industry protocols** belong to the platform. Candidates: party (person or organisation), links, timeline, documents and files, notification, calendar and availability.
   - **Industry protocols** belong to the industry package that defines them, and follow published standards where one exists: OpenTravel/HTNG for lodging, ISA-95/B2MML for manufacturing, FHIR for health, schema.org for parties.
   - A protocol is extracted when a second provider or consumer appears, never ahead of one (inner-platform risk, Platform.md §9).
6. **The same catalog faces outward.**
   - The host can expose a caller's catalog as an MCP server, so external agents discover and call apps with the caller's grants.
   - OIDC stays the identity protocol (ADR-0007).
7. **Bridges remain only for real pair-specific logic.** A bridge is justified by a mapping rule that belongs to neither side and cannot be expressed as a protocol. It is then a small adapter that provides or consumes a protocol.

**Consequences.**
- The crm-hotel bridge dissolves:
  - Hotel provides `lodging.booking/1`.
  - CRM consumes it, optionally.
  - The opportunity-to-booking link is a platform link.
  - Cancellation notes arrive through a timeline subscription to the protocol's `canceled` event.
- Any other lodging app (serviced apartments, coworking) plugs in without new code in CRM.
- Settings shows protocols with their providers and consumers instead of an app-to-app graph.
- The kernel contract is unchanged; protocols live above it, at layer 2 for cross-industry protocols and in industry packages for industry protocols.

**Revisit when** two providers of one protocol need behaviour the protocol cannot express (then the protocol grows a version), or a protocol needs to cross deployments (then it needs a wire format and conformance vectors like the kernel's).
