import type {AuthoringSection} from "../draft";
import {TableInspector,type TableInspectorPorts} from "./TableInspector";
import {RecordEventsFields} from "./RecordTimeFields";
export function RecordEventsInspector(props:Omit<TableInspectorPorts,"section"|"onChange">&{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}) {return <><TableInspector {...props} widget="record-events" showFields={false}/><RecordEventsFields info={props.info} value={props.section.recordEvents} onChange={recordEvents=>props.onChange({recordEvents})}/></>;}
