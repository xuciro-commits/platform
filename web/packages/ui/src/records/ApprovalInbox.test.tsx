import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import {afterEach,expect,test,vi} from "vitest";
import {pageUIManifest,type Api} from "@platform/kernel";
import {ApprovalInbox,type ApprovalInboxRow} from "./ApprovalInbox";
afterEach(cleanup);
const stamp={by:"original-actor",at:"2026-10-03T12:00:00.123456789Z"};
function row(id:string):ApprovalInboxRow {return {task:{id:`TASK-${id}`,revision:2,created:stamp,changed:stamp,title:"Same offered title",ref:`work.approval/REQUEST-${id}`,app:"work",candidates:["reviewer"],state:"open"},request:{id:`REQUEST-${id}`,revision:7,created:stamp,changed:stamp,title:"<b>Original request</b>",action:"build.drawing.release",target:`build.drawing/${id}`,app:"build",requester:"actual-requester",submission:`SUBMISSION-${id}`,level:0,levels:[{title:"Original review level",approvers:["reviewer"],approved:[]}],state:"pending"}};}
test("original tasks and requests retain independent IDs/revisions and duplicate titles emit their exact request",()=>{
 const rows=[row("A"),row("B")],approve=vi.fn(),reject=vi.fn(),open=vi.fn(),view=render(<ApprovalInbox rows={rows} total={2} onApprove={approve} onReject={reject} onOpenRequest={open}/>);expect(screen.getAllByText("Same offered title")).toHaveLength(2);expect(screen.getAllByText("<b>Original request</b>")).toHaveLength(2);expect(view.container.querySelector("b")).toBeNull();expect(screen.getByText("Task TASK-A · revision 2")).toBeTruthy();expect(screen.getAllByText(/Revision 7/)).toHaveLength(2);expect(screen.getByText("build.drawing/B")).toBeTruthy();expect(screen.getAllByText("actual-requester")).toHaveLength(2);expect(screen.getAllByText("build.drawing.release")).toHaveLength(2);
 fireEvent.click(screen.getAllByRole("button",{name:"Approve"})[1]!);expect(approve).toHaveBeenCalledWith(rows[1]!.request,rows[1]!.task);fireEvent.click(screen.getAllByRole("button",{name:"Reject"})[0]!);expect(reject).toHaveBeenCalledWith(rows[0]!.request,rows[0]!.task);fireEvent.click(screen.getAllByRole("button",{name:"Open approval request"})[0]!);expect(open).toHaveBeenCalledWith(rows[0]!.request,rows[0]!.task);expect(screen.getAllByText("Pending")).toHaveLength(2);
});
test("pending/read failure and incompatible original request metadata never expose actionable invented approvals",()=>{
 const original=row("A"),approve=vi.fn(),view=render(<ApprovalInbox rows={[{task:original.task,loading:true}]} onApprove={approve}/>);expect(screen.getAllByRole("status").at(-1)!.textContent).toBe("Confirming original approval request…");expect(screen.queryByRole("button")).toBeNull();
 view.rerender(<ApprovalInbox rows={[{task:original.task,error:"Not readable"}]} onApprove={approve}/>);expect(screen.getByRole("alert").textContent).toBe("Not readable");expect(screen.queryByText("actual-requester")).toBeNull();
 for(const request of [{...original.request!,id:"wrong"},{...original.request!,revision:Number.MAX_SAFE_INTEGER+1},{...original.request!,state:"pending",level:9},{...original.request!,action:""},{...original.request!,archived:true}] as Api.ApprovalRequest[]){view.rerender(<ApprovalInbox rows={[{task:original.task,request}]} onApprove={approve}/>);expect(screen.getByRole("alert").textContent).toContain("original approval request");expect(screen.queryByRole("button")).toBeNull();}
 expect(approve).not.toHaveBeenCalled();
});
test("readonly, busy and completed requests cannot approve while mutation refusal retains the original actionable row",()=>{
 const original=row("A"),approve=vi.fn(),view=render(<ApprovalInbox rows={[original]} onApprove={approve} enabled={false}/>);const button=screen.getByRole("button",{name:"Approve"});fireEvent.click(button);expect(approve).not.toHaveBeenCalled();expect((button as HTMLButtonElement).disabled).toBe(true);
 view.rerender(<ApprovalInbox rows={[original]} onApprove={approve} busyIDs={[original.request!.id]}/>);expect((button as HTMLButtonElement).disabled).toBe(true);expect(screen.getAllByRole("status").at(-1)!.textContent).toBe("Confirming approval decision…");
 view.rerender(<ApprovalInbox rows={[{...original,request:{...original.request!,state:"approved"}}]} onApprove={approve}/>);expect(screen.getByText("Approved")).toBeTruthy();fireEvent.click(button);expect(approve).not.toHaveBeenCalled();
 view.rerender(<ApprovalInbox rows={[original]} onApprove={approve} errors={["REQUEST-A"].reduce((errors,id)=>({...errors,[id]:"Refused by the host"}),{})}/>);expect(screen.getByRole("alert").textContent).toBe("Refused by the host");expect(screen.getByText("actual-requester")).toBeTruthy();expect((button as HTMLButtonElement).disabled).toBe(false);fireEvent.click(button);expect(approve).toHaveBeenCalledWith(original.request,original.task);
 view.rerender(<ApprovalInbox rows={[original]}/>);expect(screen.queryByRole("button")).toBeNull();
});
test("known Go null approved slices remain empty without fabricated decisions and delegate keys use original own properties",()=>{
 const original=row("A"),request={...original.request!,levels:[{...original.request!.levels[0]!,approved:null}]} as unknown as Api.ApprovalRequest,view=render(<ApprovalInbox rows={[{task:original.task,request}]} onApprove={()=>{}}/>);expect(screen.getByRole("button",{name:"Approve"})).toBeTruthy();expect(screen.queryByText(/approved by/)).toBeNull();
 view.rerender(<ApprovalInbox rows={[{task:original.task,request:{...original.request!,levels:[{...original.request!.levels[0]!,approved:["constructor"],decidedBy:{}}]}}]}/>);expect(screen.getByText("approved by constructor")).toBeTruthy();expect(screen.queryByText(/function Object/)).toBeNull();
 view.rerender(<ApprovalInbox rows={[{task:original.task,request:{...original.request!,levels:[{...original.request!.levels[0]!,approved:["reviewer"],decidedBy:{reviewer:"actual-delegate"}}]}}]}/>);expect(screen.getByText("approved by actual-delegate for reviewer")).toBeTruthy();
});
test("caller-owned decision eligibility blocks both decisions while retaining original metadata and navigation",()=>{
 const original=row("A"),approve=vi.fn(),reject=vi.fn(),openRequest=vi.fn(),openRelated=vi.fn();
 render(<ApprovalInbox rows={[{...original,canDecide:false,decisionReason:"Your original approval was already recorded."}]} onApprove={approve} onReject={reject} onOpenRequest={openRequest} onOpenRelated={openRelated}/>);
 expect(screen.getAllByRole("status").at(-1)!.textContent).toBe("Your original approval was already recorded.");
 expect(screen.getByText("Task TASK-A · revision 2")).toBeTruthy();expect(screen.getByText("build.drawing/A")).toBeTruthy();expect(screen.getByText("actual-requester")).toBeTruthy();
 for(const name of ["Approve","Reject"]){const button=screen.getByRole("button",{name});expect((button as HTMLButtonElement).disabled).toBe(true);fireEvent.click(button);}
 expect(approve).not.toHaveBeenCalled();expect(reject).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"Open approval request"}));expect(openRequest).toHaveBeenCalledWith(original.request,original.task);
 fireEvent.click(screen.getByRole("button",{name:"Open related record"}));expect(openRelated).toHaveBeenCalledWith(original.request,original.task);
});
test("approval task budgets and loaded totals are explicit and malformed or duplicate task identities refuse the window",()=>{
 const original=row("A"),view=render(<ApprovalInbox rows={[original]} total={20} loadedCount={20}/>);expect(screen.getByRole("status").textContent).toBe("Showing 1 of 20 approval tasks.");
 view.rerender(<ApprovalInbox rows={[original]}/>);expect(screen.getByRole("status").textContent).toContain("complete total is unavailable");for(const rows of [[original,original],[{...original,task:{...original.task,ref:"sample.asset/A"}}],Array.from({length:pageUIManifest.runtime.workViews.maxApprovalsWindow+1},(_,index)=>row(String(index)))]){view.rerender(<ApprovalInbox rows={rows}/>);expect(screen.getByRole("alert").textContent).toContain("approval task window");expect(screen.queryByRole("list")).toBeNull();}
});
