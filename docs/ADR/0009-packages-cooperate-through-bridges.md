# ADR-0009: Business packages cooperate through bridge packages

**Status:** Superseded by ADR-0011 (2026-09-24): apps meet through protocols; a bridge remains only for pair-specific logic, and the crm-hotel bridge was removed in #95.

**Context.** The product intent review asks that business capabilities combine into complete software without packages depending on each other's internals. The first composition joined CRM and Hotel: a sales rep books stays for a customer's opportunity and sees them on the customer.

**Decision.**
1. A business package depends on the platform only. It offers declared actions (ADR-0008) and public reads; nothing else of it is visible to other packages.
2. What cooperation adds belongs to a bridge package (Odoo's bridge modules): it imports the packages it joins, owns the cooperation's own entities and decisions, and reaches each package only through its actions and reads.
3. A bridge action targets an entity the bridge owns (one authority per data class, K5). When it needs another package's decision, that decision runs as the bridge decision's rule (K4 C10): a refusal refuses the bridge decision with the same code, and the called package keeps its own roles, rules and revisions. The called submission's idempotency key is derived from the bridge's, so a resend never acts twice.
4. A bridge action appears in a caller's catalog only when every package it calls would accept that caller.
5. A package's UI is a package too (`@pkg/<name>`): every software that shows its data uses its views and model.

**Consequences.** Packages stay independent (`verify.sh composition` checks it), and a composed software is its packages plus bridges plus composition code for routing and members (F-21, F-23 until a package host exists). Causation between logs is carried by derived keys and correlation IDs (F-22). Reads across packages are live, so a hotel cancellation shows on the customer at once, but a bridge cannot yet react to another package's decisions (no subscriptions).

**Revisit when** a bridge must react to another package's decisions (subscriptions over change records), or two bridges need the same cooperation (it may belong in one of the packages).
