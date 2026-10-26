import {Markdown,PageHeader} from '@platform/ui';
export function TextRenderer({text}:{text?:string}){return <Markdown content={text} className="text-sm"/>;}
export function HeadingRenderer({level,title}:{level:1|2|3;title:string}){return <PageHeader compact level={level} title={title}/>;}
