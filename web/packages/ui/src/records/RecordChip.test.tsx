import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { RecordChip } from "./RecordChip";
import { type EntityInfo, type RecordSource } from "./Records";

const entity: EntityInfo = { type: "crm.customer", app: "crm", title: "Customer", plural: "Customers", primary: "id",
  fields: [{ name: "id", title: "ID", type: "text", readOnly: true }, { name: "name", title: "Name", type: "text" }], display: "name" } as never;

const sourceWith = (scope: string, title: string | undefined, failure?: boolean): RecordSource => ({
  scope, revision: 1, entity: () => entity, list: vi.fn(),
  get: failure ? vi.fn(() => Promise.reject(new Error("denied"))) : vi.fn(() => Promise.resolve({ record: { id: "c1", name: title, revision: 1 }, info: entity } as never)),
});

describe("RecordChip", () => {
  afterEach(() => { cleanup(); vi.restoreAllMocks(); });

  test("resolves the display title through the caller's own source", async () => {
    render(<RecordChip source={sourceWith("tenant-a", "Acme")} type="crm.customer" id="c1" />);
    await waitFor(() => expect(screen.getByRole("button").textContent).toBe("Acme"));
  });

  test("the same type and id under another identity never reuses the first title", async () => {
    const { rerender } = render(<RecordChip source={sourceWith("tenant-a", "Acme")} type="crm.customer" id="c1" />);
    await waitFor(() => expect(screen.getByRole("button").textContent).toBe("Acme"));
    const denied = sourceWith("tenant-b", undefined, true);
    rerender(<RecordChip source={denied} type="crm.customer" id="c1" />);
    await waitFor(() => expect(denied.get).toHaveBeenCalled());
    expect(screen.getByRole("button").textContent).not.toBe("Acme");
    expect(screen.getByRole("button").textContent).toBe("c1");
  });

  test("a failure is not permanent: the next source revision resolves again", async () => {
    const source: RecordSource = { scope: "tenant-a", revision: 1, entity: () => entity, list: vi.fn(),
      get: vi.fn().mockRejectedValueOnce(new Error("down")).mockResolvedValue({ record: { id: "c1", name: "Recovered", revision: 1 }, info: entity } as never) };
    const { rerender } = render(<RecordChip source={source} type="crm.customer" id="c1" />);
    await waitFor(() => expect(source.get).toHaveBeenCalledTimes(1));
    (source as { revision: number }).revision = 2;
    rerender(<RecordChip source={source} type="crm.customer" id="c1" />);
    await waitFor(() => expect(screen.getByRole("button").textContent).toBe("Recovered"));
  });
});
