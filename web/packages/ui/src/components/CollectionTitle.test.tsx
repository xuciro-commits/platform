import {cleanup,render,screen} from "@testing-library/react";
import {afterEach,expect,test} from "vitest";
import {CollectionTitle} from "./CollectionTitle";
import {PageHeader} from "./PageHeader";
afterEach(cleanup);
test("headers use literal text and declared semantic levels",()=>{const {rerender}=render(<PageHeader compact level={2} title="<b>Literal text</b>"/>);expect(screen.getByRole("heading",{level:2}).textContent).toBe("<b>Literal text</b>");expect(document.querySelector("b")).toBeNull();rerender(<PageHeader compact level={3} title="Smaller heading"/>);expect(screen.getByRole("heading",{level:3}).textContent).toBe("Smaller heading");});
test("collection title replaces its count with pending or denied status and preserves real zero",()=>{const {rerender}=render(<CollectionTitle title="Assets" value="620"/>);expect(screen.getByRole("status").textContent).toBe("620");rerender(<CollectionTitle title="Assets"/>);expect(screen.queryByText("620")).toBeNull();expect(screen.getByRole("status").textContent).toContain("Loading");rerender(<CollectionTitle title="Assets" error="Denied"/>);expect(screen.getByRole("alert").textContent).toBe("Denied");expect(screen.queryByText("620")).toBeNull();rerender(<CollectionTitle title="Assets" value="0"/>);expect(screen.getByRole("status").textContent).toBe("0");});
