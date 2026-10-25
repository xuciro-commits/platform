import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { RecordChip } from "./RecordChip";
import { type EntityInfo, type RecordSource } from "./Records";
import { ViewVisibilityContext } from "../shell/ViewVisibility";

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
    let refuse!:(error:Error)=>void;
    const denied = {...sourceWith("tenant-b", undefined),get:vi.fn(()=>new Promise<never>((_,reject)=>{refuse=reject;}))};
    rerender(<RecordChip source={denied} type="crm.customer" id="c1" />);
    expect(screen.getByRole("button").textContent).toBe("c1");
    await waitFor(() => expect(denied.get).toHaveBeenCalled());
    await act(async()=>refuse(new Error("denied")));
    expect(screen.getByRole("button").textContent).not.toBe("Acme");
    expect(screen.getByRole("button").textContent).toBe("c1");
  });

  test("unscoped sources do not reuse another source's title or late response", async()=>{
    let resolve!:(value:never)=>void;
    const first={...sourceWith("unused","Acme"),scope:undefined,get:vi.fn(()=>new Promise<never>(done=>{resolve=done;}))};
    const second={...sourceWith("unused","Other"),scope:undefined};
    const {rerender}=render(<RecordChip source={first} type="crm.customer" id="c1"/>);
    rerender(<RecordChip source={second} type="crm.customer" id="c1"/>);
    await waitFor(()=>expect(screen.getByRole("button").textContent).toBe("Other"));
    await act(async()=>resolve({record:{id:"c1",name:"Old"}} as never));
    expect(screen.getByRole("button").textContent).toBe("Other");
  });

  test("renames and revocation follow the original watch and hidden views release it",async()=>{
    let changed!:()=>void;
    const stop=vi.fn(),source={...sourceWith("member","Acme"),watchRecord:vi.fn((_type:string,_id:string,next:()=>void)=>{changed=next;return stop;})};
    const open=vi.fn(),{rerender}=render(<ViewVisibilityContext value={true}><RecordChip source={source} type="crm.customer" id="c1" onOpen={open}/></ViewVisibilityContext>);
    await waitFor(()=>expect(screen.getByRole("button").textContent).toBe("Acme"));
    fireEvent.click(screen.getByRole("button"));expect(open).toHaveBeenCalledWith("crm.customer","c1");
    vi.mocked(source.get).mockResolvedValueOnce({record:{id:"c1",name:"Renamed"}} as never);
    await act(async()=>changed());expect(screen.getByRole("button").textContent).toBe("Renamed");
    vi.mocked(source.get).mockRejectedValueOnce(Error("denied"));
    await act(async()=>changed());expect(screen.getByRole("button").textContent).toBe("c1");
    rerender(<ViewVisibilityContext value={false}><RecordChip source={source} type="crm.customer" id="c1"/></ViewVisibilityContext>);
    expect(stop).toHaveBeenCalledTimes(1);expect((screen.getByRole("button") as HTMLButtonElement).disabled).toBe(true);
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
