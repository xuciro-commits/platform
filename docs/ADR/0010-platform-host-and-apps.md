# ADR-0010: The platform is a host that runs apps; its own administration is an app

**Status:** Proposed (2026-09-24; awaiting the owner)

**Context.** Composition #91 worked, but by hand: each package defines its own principal (F-21), routing across packages is composition code (F-23), and no journal spans packages. The owner's model is an operating system. The platform is the base layer. Business packages are apps. The platform's own administration (identity, roles, apps, connectors, data) is an app too, like Settings. Apps offer capabilities to each other, and what an app offers must be discoverable without the dependencies turning into a tangle.

**Decision.**

1. **Host.** One platform host per deployment runs a set of apps for each tenant. It owns everything apps share:
   - authentication (OIDC) and the tenant directory;
   - one ordered journal per tenant across all its apps;
   - the kernel wiring (`Ledger`);
   - routing of submissions and reads to the owning app;
   - the catalog each caller receives;
   - audit.

   Apps never wire the kernel, authenticate, or route.
2. **App manifest.** Each app declares itself in typed code (not configuration):
   - identity and version;
   - the data classes it is authority for;
   - its actions (ADR-0008) and public reads;
   - the roles it defines;
   - the other apps' actions and reads it requires;
   - the capabilities that can be deactivated;
   - its connectors;
   - its UI package.

   A bridge (ADR-0009) is an app whose manifest requires two apps.
3. **Three tiers, one direction.** Platform services ← business apps ← bridges and solution apps.
   - A business app requires only the platform.
   - A bridge requires the apps it joins.
   - An app that requires another app's actions must declare it, and the requirement graph must be acyclic.
   - The host refuses to start a tenant whose enabled apps leave a requirement unmet. A verify gate checks the graph at build time.
4. **Calls go through the host.** An app reaches another app only through the host, by action name or read name, never through its Go internals. So every cross-app call is:
   - authorized with the caller's roles in the called app;
   - ordered in the tenant journal;
   - audited;
   - visible in the dependency graph.

   Discovery is the host's registry of manifests, filtered per caller. People, integrations and AI agents all use the same `/v1/actions`. There is no runtime service lookup: what exists is fixed at build time (ADR-0008).
5. **Directory.** A member of a tenant has one role per app, plus attributes (lines, properties). Granting and revoking are decisions of the platform app (K4), with history. They take effect on the next request, which closes the operations-floor gap on revocation. The member replaces per-package principals (F-21).
6. **The platform app.** It runs on the host like any app, with a web "Settings" workspace:
   - tenants;
   - members and their role per app;
   - the apps enabled per tenant, with their requirement graph;
   - AI agents and their grants;
   - connectors and their health;
   - journal and audit views;
   - the live capability matrix: what each app provides and requires, read from the registry.

   Its own actions go through the kernel like any app's.
7. **Platform services, added when an app needs them:**
   - events: subscriptions over change records, delivered after commit, handlers owned as work (K9);
   - files;
   - analysis datasets: read-only projections for customer models and dashboards (ADR-0008);
   - notifications.

   Each service is a platform capability with its own row in the matrix; none is built ahead of a user.

**Consequences.** #92 becomes the first step of this ADR: the host, manifests, the directory and the tenant journal, with the sales composition and manufacturing moved onto it. The admin app follows; then events. The kernel contract does not change. The manifest may become contract (spec and vectors) once a non-Go app needs it. Composition code in `crmhotel.NewServer` and the per-package principal types are deleted.

**Revisit when** an app must run in its own process or be released on its own (the host then becomes a gateway over app processes), or third parties build apps (ADR-0008 point 1).
