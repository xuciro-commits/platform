# ADR-0030: Production progress — the plant's status reaches the ERP, confirmed lot by lot

**Status:** Proposed (2026-09-27, the owner's testing of route 1, #125)

## Context

The owner's testing (route 1, 2026-09-27):
- A shop order's SFCs were in work while the ERP's production order still read "released": what happens downstream does not reach the order upstream until the very end.
- An order of 10 against a planned order of 20 could not be confirmed, and the correction agent found no planned order to propose, because a confirmation is all or nothing: `production.orders/1#confirm` (`protocols/production`) confirms a planned order once, and the ERP app (`apps/erp/server/erp.go`) moves it from released to confirmed with one posting of the whole yield.
- The owner asked whether plants and ERPs confirm in parts: an order of fifty million is not received only when the last unit is made.

What exists: the protocol has one action, `confirm` (shop order, yield, scrap), one read (`orders`) and one event (`released`). The MES confirms when a shop order's last SFC ends (the `mes.erp-confirmation` flow, ADR-0026 9c). The ERP app posts the yield at standard cost and numbers it; the ERP adapter (7d) sends the confirmation to an ERP outside as an effect.

What the reference platforms do now:

| | Status up | Partial confirmation | Receipt |
|---|---|---|---|
| SAP S/4HANA with SAP Digital Manufacturing | The MES sends each operation's and order's status; the ERP sets its order status from them ([Help Portal](https://help.sap.com/docs/sap-digital-manufacturing/integration-guide/automatic-confirmation-integration)) | A partial yield confirms the order partially (status PCNF) until the confirmed quantity reaches the target (CNF) ([Posting confirmations](https://learning.sap.com/courses/implementing-sap-s-4hana-cloud-public-edition-manufacturing/posting-confirmations-for-production-orders-1)) | Each confirmation may post the goods receipt of its yield at once |
| ISA-95 (the protocol's model) | Production performance reported as it happens | Performance per segment and lot | — |

They agree: the plant reports as it goes — started, each quantity made — and the ERP keeps the order's confirmed quantity, its status and the stock received, confirmation by confirmation, closing the order when the target is reached or the plant says it is final.

## Our constraints

- One decision changes one app (ADR-0026): the ERP's order moves by the ERP's own decisions on what the protocol carries; the MES never writes the ERP's records, nor reads them to decide.
- Protocols are versioned (ADR-0011): an addition every provider can take stays `production.orders/1`; a change of meaning is `/2`.
- Replay never calls outside; the adapter's calls stay effects.
- No new dependency.

## Design

1. **Started.** A new action, `production.orders/1#start` (shop order): the plant's first SFC to start on an order sends it; the ERP's order moves from released to in process. The adapter sends it to the outside ERP as an effect, like a confirmation.
2. **Confirmation in parts.** `confirm` gains `final` (boolean, default false). The ERP keeps on the order its confirmed yield and scrap and each confirmation (number, shop order, yield, scrap, posting); a confirmation posts the receipt of its yield at standard cost at once; the order is partly confirmed until the confirmed yield reaches its quantity or a confirmation is final, then confirmed. A yield past the order's quantity is refused, with why (F-23).
3. **The plant confirms per lot.** The MES confirms each SFC's quantity when it ends (an SFC now carries its quantity), final with the shop order's last SFC; a scrapped SFC confirms its quantity as scrap. The confirmation flow runs per lot; its correction and resend stay as they are.
4. **Several shop orders on one planned order.** A shop order may name a planned order with open quantity (its quantity less what is confirmed) at least its own; the correction agent proposes those. The protocol's `orders` read carries the confirmed quantity.
5. **The principle, for every app.** A document downstream moves the one upstream only through what the protocol between them carries, as each step happens — not once at the end. The CRM's stays (ADR-0026) already work so; purchasing and receipts are one app (the ERP) and move each other directly.

## Decision points for the owner

| # | Question | Options | Recommendation |
|---|---|---|---|
| D1 | Status up | (a) `start` when the first SFC starts; (b) also each operation's progress; (c) only confirmations | **(a)**: the ERP needs "in process"; operation detail stays the plant's |
| D2 | When the plant confirms | (a) each SFC (lot) as it ends; (b) each shop order as it ends, as today; (c) a supervisor's choice | **(a)**: receipts follow production, as the owner asked, and a lot is the unit the plant tracks |
| D3 | Over-confirmation | (a) refuse a yield past the order's quantity; (b) a tolerance per product | **(a)** now; (b) when a product needs it |
| D4 | Version | (a) extend `production.orders/1` (a new action and an optional field); (b) `production.orders/2` | **(a)**: both providers are ours and take it; an outside ERP behind the adapter sees partial confirmations, which SAP accepts |
| D5 | Receipt | (a) each confirmation posts its yield's receipt; (b) one receipt when the order closes | **(a)**: stock is there when it is made |

## Build items after the decisions

| Batch | Item | Done when |
|---|---|---|
| 13a | `start`, partial confirmation, per-lot confirmation, open quantity | The manufacturing test: an order of 10 in two lots on a planned order of 20 reads in process when the first lot starts, partly confirmed with a receipt of 5 after the first lot, and a second shop order of 10 on the same planned order confirms it; with the ERP adapter the confirmations leave as effects; `CheckReplay`; the rehearsal; Playwright shows the ERP order's confirmed quantity |

## Consequences

- The ERP knows the order is in work and receives stock as lots finish.
- A confirmation is smaller and more frequent; each has a posting and a number.
- The correction agent finds planned orders with open quantity, so a shop order smaller than its planned order is no longer an error.
