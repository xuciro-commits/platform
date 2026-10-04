import {render,screen,cleanup} from "@testing-library/react";
import {afterEach,expect,it} from "vitest";
import {ExternalFrame,validExternalFrame} from "./ExternalFrame";
afterEach(cleanup);
it("keeps every sandbox restriction and retires its browsing context when inactive",()=>{
 const config={url:"https://docs.example.com/report",origin:"https://docs.example.com",height:240},view=render(<ExternalFrame config={config} title="Original document"/>),frame=screen.getByTitle("Original document");
 expect(frame.getAttribute("sandbox")).toBe("");expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer");expect(frame.hasAttribute("srcdoc")).toBe(false);expect(frame.getAttribute("allow")).toContain("camera 'none'");
 view.rerender(<ExternalFrame config={config} title="Original document" active={false}/>);expect(screen.queryByTitle("Original document")).toBeNull();
});
it("refuses repaired URLs, unreviewed origins and executable configuration",()=>{
 for(const url of ["http://docs.example.com/report","https://user:secret@docs.example.com/report","https://docs.example.com.evil/report","https://docs.example.com/%2e%2e/secret","https://docs.example.com/with space","javascript:alert(1)"])expect(validExternalFrame({url,origin:"https://docs.example.com"})).toBe(false);
 expect(validExternalFrame({url:"https://docs.example.com/report",origin:"https://docs.example.com",sandbox:"allow-scripts"})).toBe(false);
});
