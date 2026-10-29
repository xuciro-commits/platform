import {useId,type ReactNode} from 'react';
import {CircleHelp,X} from 'lucide-react';
import {Popover} from 'radix-ui';
import {Button} from '../primitives/button';
import {t} from '../i18n';

export function ContextHelp({title,children}:{title:string;children:ReactNode}){
  const heading=useId();
  return <Popover.Root>
    <Popover.Trigger asChild><Button variant="ghost" size="sm" className="w-fit shrink-0 text-muted" aria-label={t('About {title}',{title})}><CircleHelp aria-hidden="true"/>{t('View help')}</Button></Popover.Trigger>
    {
}
    <Popover.Portal><Popover.Content onPointerDown={event=>event.stopPropagation()} side="bottom" align="start" sideOffset={6} collisionPadding={12} aria-labelledby={heading} className="z-50 grid max-h-[min(70vh,32rem)] w-96 max-w-[calc(100vw-1.5rem)] gap-3 overflow-auto rounded-md border border-border bg-surface p-4 text-sm shadow-lg">
      <div className="flex items-start justify-between gap-3"><h2 id={heading} className="font-semibold">{title}</h2><Popover.Close asChild><Button size="icon" variant="ghost" aria-label={t('Close')}><X aria-hidden="true"/></Button></Popover.Close></div>
      <div className="grid max-w-prose gap-3 break-words leading-relaxed text-muted">{children}</div>
    </Popover.Content></Popover.Portal>
  </Popover.Root>;
}
