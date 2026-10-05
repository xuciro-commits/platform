import type {AuthoringSection} from "../draft";
import {TableInspector,type TableInspectorPorts} from "./TableInspector";
import {RecordGanttFields} from "./RecordTimeFields";
export function RecordGanttInspector(props:Omit<TableInspectorPorts,"section"|"onChange">&{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}) {return <><TableInspector {...props} widget="record-gantt" showFields={false}/><RecordGanttFields info={props.info} value={props.section.recordGantt} onChange={recordGantt=>props.onChange({recordGantt})}/></>;}
