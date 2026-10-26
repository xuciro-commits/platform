import {t} from '@platform/ui';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {TextRenderer,HeadingRenderer} from './Content';
import type {SpacerRenderer} from './Spacer';
import type {SeparatorRenderer} from './Separator';
import type {NoticeRenderer} from './Notice';

const define=defineWidgetPlugin<WidgetBindingContext>();
export const contentPlugins={
 text:define('text',1,({section}):Parameters<typeof TextRenderer>[0]=>({text:section.text}),()=>import('./Content').then(m=>m.TextRenderer)),
 heading:define('heading',1,({section}):Parameters<typeof HeadingRenderer>[0]=>({level:Number(section.headingLevel?.slice(1)??2) as 1|2|3,title:section.text||t('Heading')}),()=>import('./Content').then(m=>m.HeadingRenderer)),
 spacer:define('spacer',1,({section}):Parameters<typeof SpacerRenderer>[0]=>({config:section.spacer}),()=>import('./Spacer').then(m=>m.SpacerRenderer)),
 separator:define('separator',1,({section}):Parameters<typeof SeparatorRenderer>[0]=>({config:section.separator,name:section.title||t('Separator')}),()=>import('./Separator').then(m=>m.SeparatorRenderer)),
 notice:define('notice',1,({section}):Parameters<typeof NoticeRenderer>[0]=>({config:section.notice,label:section.title||t('Notice')}),()=>import('./Notice').then(m=>m.NoticeRenderer)),
};
