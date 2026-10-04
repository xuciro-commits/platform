import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,test} from 'vitest';
import {RegionPresentation} from './LayoutRegion';
afterEach(cleanup);
test('keyboard collapse preserves the original child draft and a new lifetime restores the declared initial state',()=>{
 const p={showHeader:true,collapsible:true,defaultCollapsed:false,padding:10,background:'panel',border:true};
 const {rerender}=render(<RegionPresentation key="first" title="Detail" presentation={p}><input aria-label="Original draft" defaultValue="original"/></RegionPresentation>);
 const input=screen.getByRole('textbox',{name:'Original draft'}) as HTMLInputElement;fireEvent.change(input,{target:{value:'kept draft'}});const button=screen.getByRole('button',{name:'Detail'});fireEvent.click(button);expect(button.getAttribute('aria-expanded')).toBe('false');expect(screen.queryByRole('textbox')).toBeNull();expect(input.isConnected).toBe(true);fireEvent.click(button);expect(screen.getByRole('textbox')).toBe(input);expect(input.value).toBe('kept draft');
 rerender(<RegionPresentation key="second" title="Detail" presentation={{...p,defaultCollapsed:true}}><input aria-label="Original draft" defaultValue="new lifetime"/></RegionPresentation>);expect(screen.queryByRole('textbox')).toBeNull();fireEvent.click(screen.getByRole('button',{name:'Detail'}));expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('new lifetime');
});
