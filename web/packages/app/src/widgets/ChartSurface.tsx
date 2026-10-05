import {lazy} from 'react';
import {Panel,type ChartSpec,type ChartSource} from '@platform/ui';
import type {PivotPorts} from './Pivot';
const ChartRenderer=lazy(()=>import('./Chart').then(m=>({default:m.ChartRenderer})));
const PivotRenderer=lazy(()=>import('./Pivot').then(m=>({default:m.PivotRenderer})));
export type ChartSurfaceProps={spec?:ChartSpec;source?:ChartSource;kpi:boolean;pivot?:PivotPorts;message?:string;alert?:boolean;panel?:boolean};
export function ChartSurfaceRenderer({spec,source,kpi,pivot,message,alert,panel}:ChartSurfaceProps){
 if(message)return panel?<Panel role={alert?'alert':'status'}>{message}</Panel>:<p role={alert?'alert':undefined} className={alert?'text-sm text-danger':'text-sm text-muted'}>{message}</p>;
 if(pivot)return <PivotRenderer {...pivot}/>;
 return spec?<ChartRenderer spec={spec} height={kpi?120:240} source={source}/>:null;
}
