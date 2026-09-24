# ADR-0012: Organisation is a network of units in several structures, over time

**Status:** Accepted (2026-09-24, #96; owner's partner asked for a design above departments and teams)

**Context.** ADR-0010 listed organisational units as a host capability. The owner's partner pointed out that an organisation is not a tree of departments, teams and people. A group holds subsidiaries, business groups, factories, project organisations, temporary committees and external partners, and one person often belongs to several of these structures at once. Organisation in general is wider still:

| Kind of organisation | What it shows |
|---|---|
| A living organism | The same parts (organs) belong to several systems at once (nervous, circulatory) |
| A nonprofit | Governance by a board and members, with volunteers and donors outside employment |
| An open-source project | Roles are earned (contributor → committer → maintainer), working groups overlap, identities live elsewhere, and a foundation hosts many projects |

Across all of these, six things recur:
- **Parts:** units of different kinds.
- **Structures:** several independent ways the parts relate — legal ownership, management, finance, site, project, governance, community.
- **Membership:** who belongs, how, since when and until when.
- **Authority:** which structure carries which right.
- **Boundary:** inside, outside, or shared.
- **Lifecycle:** founding, temporary existence, merger, split, dissolution.

Established models agree:
- The W3C Organization Ontology (ORG) models organisations, units, collaborations, memberships with a role and an interval, posts, sites and change events.
- Workday keeps separate supervisory, company, cost-center, region and matrix hierarchies.
- SAP HCM relates organisational units, positions and persons, each relation with a validity period.
- FHIR relates organisations to each other and to practitioners through affiliations.

**Decision.** The platform's organisation capability (layer 2, the `org` app of every tenant) models these generically. Industries name the kinds; the kernel does not change (K6: organisation stays context for policy, not kernel schema).

1. **Units.** A unit is any organisation or part of one: a group, subsidiary, business group, factory, line, department, team, project, committee, working group, an external partner, a whole other organisation. Its kind is open vocabulary, chosen by the tenant or the industry, never a fixed list. Its facets are independent of its kind:
   - **Legal:** is it a legal entity?
   - **Boundary:** internal or external.
   - **Lifespan:** valid from, and until (temporary units carry an end date).
2. **Structures.** A structure is one named way units relate. Units belong to any number of structures, with different parents in each.
   - **Kinds:** legal (ownership, with a share), management (reports to), finance (cost or profit center), site (located at), project, governance (committees, boards), community, or custom.
   - **Links:** each unit links to its parent in a structure through an edge valid from/until. Structures are trees by default; a structure may allow several parents (a matrix).
   - **Example:** a factory can belong legally to subsidiary A, be managed by business group B, and sit in region C.
3. **Memberships.** A membership links a party to a unit.
   - **Who:** a member of the directory (person, service, AI agent) or another unit (an organisation as a member of a consortium or foundation).
   - **What:** a role (employee, contractor, volunteer, maintainer, delegate, observer, chair — open vocabulary), a primary flag, and a validity interval.
   - One party may hold any number of memberships across structures at the same time.
4. **Time.** Every unit, edge and membership has valid time, so both of these can be answered:
   - who belonged where on a past date;
   - a reorganisation that takes effect next month.

   A change is a decision of the `org` app (K4) with history, journaled like any other. A merger or split closes units and records their successors (K1 redirects when references must keep resolving).
5. **Authority follows a named structure.** Apps do not read org charts; they ask the host for *the units a member belongs to in structure S, at time T, including everything below them* (`Caller.Units`). Each rule states which structure carries it:
   - a line operator's scope comes from the site structure;
   - an approval limit comes from the management structure;
   - a cost report comes from the finance structure.

   This replaces ad hoc member attributes such as `lines`.
6. **The organisation is data, not code.** Industry packages ship their unit and structure kinds and a starting shape. Tenants change their organisation in Settings as decisions, within ADR-0008: the rules stay code, the organisation is theirs.

**Consequences.**
- Manufacturing's lines become units of the plant's site structure. Operators and agents belong to lines, supervisors to the plant, and the plant's rules ask for the member's site units instead of a `lines` attribute.
- Settings gains Organisation:
  - each structure as a tree, at a chosen date;
  - a unit with its members;
  - a person with every membership across structures.
- Not yet: posts (positions independent of their holders, as in SAP HCM), delegation of authority between units, and federation with another tenant's organisation. Each comes with its first real need.

**Revisit when** a rule needs a structure that is not a tree or matrix (a network of peers), or organisations of two tenants must share units (federation).
