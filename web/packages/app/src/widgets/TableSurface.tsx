import {lazy} from 'react';
import {Panel,t} from '@platform/ui';
import {useOpenRecord} from '../index';
import type {TablePorts} from './Table';
import type {RecordTilesRenderer} from './RecordWork';
const TableRenderer=lazy(()=>import('./Table').then(m=>({default:m.TableRenderer})));
const TilesRenderer=lazy(()=>import('./RecordWork').then(m=>({default:m.RecordTilesRenderer})));
export type TableSurfaceProps={ports?:TablePorts;tiles?:Parameters<typeof RecordTilesRenderer>[0];tableKey?:string;message?:string;navigate?:boolean};
export function TableSurfaceRenderer({ports,tiles,tableKey,message,navigate}:TableSurfaceProps){
 if(message)return <Panel role="alert">{t(message)}</Panel>;
 if(tiles)return <TilesRenderer {...tiles}/>;
 return ports?<BoundTable ports={ports} tableKey={tableKey} navigate={navigate}/>:null;
}
function BoundTable({ports,tableKey,navigate}:{ports:TablePorts;tableKey?:string;navigate?:boolean}){
 const open=useOpenRecord();
 return <TableRenderer key={tableKey} {...ports} onNavigate={navigate?record=>open({type:ports.object,id:record.id}):undefined}/>;
}
