import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {BreadcrumbTrail,type BreadcrumbTrailItem} from "./BreadcrumbTrail";
import {pageUIManifest} from "@platform/kernel";
afterEach(cleanup);
const items:BreadcrumbTrailItem[]=[{id:"home",label:"Same title"},{id:"page",label:"Same title"},{id:"sample.person/A",label:"<b>Original record</b>",detail:"sample.person/A"}];
test("duplicate labels retain original breadcrumb identities and current record cannot navigate",()=>{
 const activate=vi.fn(),view=render(<BreadcrumbTrail items={items} currentID={items[2]!.id} onActivate={activate}/>);
 expect(screen.getByRole("navigation",{name:"Breadcrumbs"})).toBeTruthy();expect(screen.getAllByRole("button",{name:"Same title"})).toHaveLength(2);expect(screen.getByText("<b>Original record</b>")).toBeTruthy();expect(screen.getByText("sample.person/A")).toBeTruthy();expect(view.container.querySelector("b")).toBeNull();expect(view.container.querySelector('[aria-current="page"]')!.textContent).toContain("sample.person/A");
 fireEvent.click(screen.getAllByRole("button",{name:"Same title"})[1]!);expect(activate).toHaveBeenCalledWith(items[1]);fireEvent.click(screen.getAllByRole("button",{name:"Same title"})[0]!);expect(activate).toHaveBeenCalledWith(items[0]);expect(screen.queryByRole("button",{name:/Original record/})).toBeNull();
});
test("readonly and disabled trails retain original labels without callbacks, and controlled current identity changes",()=>{
 const activate=vi.fn(),view=render(<BreadcrumbTrail items={items} currentID="page" onActivate={activate} enabled={false}/>);const home=screen.getByRole("button",{name:"Same title"});expect((home as HTMLButtonElement).disabled).toBe(true);fireEvent.click(home);expect(activate).not.toHaveBeenCalled();
 view.rerender(<BreadcrumbTrail items={items.slice(0,2)} currentID="page"/>);expect(screen.queryByRole("button")).toBeNull();expect(view.container.querySelector('[aria-current="page"]')!.textContent).toBe("Same title");
});
test("an explicit current-page clear is separate from ancestor navigation and remains controlled when disabled",()=>{
 const originalItems=[{id:"home",label:"Home"},{id:"page",label:"Original page"}],clear=vi.fn(),navigate=vi.fn(),view=render(<BreadcrumbTrail items={originalItems} currentID="page" onActivate={navigate} onCurrentActivate={clear}/>);
 const current=screen.getByRole("button",{name:"Original page"});expect(current.getAttribute("aria-current")).toBe("page");fireEvent.click(current);expect(clear).toHaveBeenCalledTimes(1);expect(navigate).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"Home"}));expect(navigate).toHaveBeenCalledWith(originalItems[0]);expect(clear).toHaveBeenCalledTimes(1);
 view.rerender(<BreadcrumbTrail items={originalItems} currentID="page" onActivate={navigate} onCurrentActivate={clear} enabled={false}/>);expect((current as HTMLButtonElement).disabled).toBe(true);fireEvent.click(current);expect(clear).toHaveBeenCalledTimes(1);expect(navigate).toHaveBeenCalledTimes(1);
 view.rerender(<BreadcrumbTrail items={originalItems} currentID="page" onActivate={navigate}/>);expect(screen.queryByRole("button",{name:"Original page"})).toBeNull();expect(view.container.querySelector('[aria-current="page"]')!.tagName).toBe("SPAN");
});
test("missing current identity, duplicate original identities and label budgets fail explicitly",()=>{
 const view=render(<BreadcrumbTrail items={items} currentID="wrong"/>);expect(screen.getByRole("alert")).toBeTruthy();
 view.rerender(<BreadcrumbTrail items={[items[0]!,items[0]!]} currentID="home"/>);expect(screen.getByRole("alert")).toBeTruthy();
 view.rerender(<BreadcrumbTrail items={[{id:"home",label:"字".repeat(pageUIManifest.runtime.contextViews.maxLabelBytes)}]} currentID="home"/>);expect(screen.getByRole("alert")).toBeTruthy();expect(screen.queryByRole("navigation")).toBeNull();
});
