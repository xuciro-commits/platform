import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {RecordGanttRenderer} from './RecordGantt';
import type {RecordCalendarRenderer} from './RecordCalendar';
import type {RecordEventsRenderer} from './RecordEvents';
import type {RecordScatterRenderer} from './RecordScatter';
import type {LeaderboardRenderer} from './Leaderboard';
const define=defineWidgetPlugin<WidgetBindingContext>();
export const recordWindowPlugins={
 'record-gantt':define('record-gantt',1,({section,window,info}):Parameters<typeof RecordGanttRenderer>[0]=>({window,info,fields:section.recordGantt}),()=>import('./RecordGantt').then(m=>m.RecordGanttRenderer)),
 'record-calendar':define('record-calendar',1,({section,window,info,selected,onSelect}):Parameters<typeof RecordCalendarRenderer>[0]=>({window,info,fields:section.recordCalendar,selected,onSelect}),()=>import('./RecordCalendar').then(m=>m.RecordCalendarRenderer),({sourceScope,page,section,window})=>JSON.stringify([sourceScope,section.object?.name||page.object.name,section.recordCalendar,window?.query])),
 'record-events':define('record-events',1,({section,window,info}):Parameters<typeof RecordEventsRenderer>[0]=>({window,info,fields:section.recordEvents}),()=>import('./RecordEvents').then(m=>m.RecordEventsRenderer)),
 'record-scatter':define('record-scatter',1,({section,window,info,selected,enabled,onSelect,pickerConfirmation}):Parameters<typeof RecordScatterRenderer>[0]=>({window,info,fields:section.scatter,confirmation:pickerConfirmation,selected,enabled,onSelect}),()=>import('./RecordScatter').then(m=>m.RecordScatterRenderer)),
 'record-leaderboard':define('record-leaderboard',1,({section,window,info,selected,onSelect}):Parameters<typeof LeaderboardRenderer>[0]=>({window,info,fields:section.leaderboard,selected,onSelect}),()=>import('./Leaderboard').then(m=>m.LeaderboardRenderer)),
};
