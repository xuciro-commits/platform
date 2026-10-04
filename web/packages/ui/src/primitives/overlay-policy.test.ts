import {describe,test,expect,vi} from 'vitest';import {overlayPolicy} from './overlay-policy';
describe('shared overlay dismissal policy',()=>{test('Escape and backdrop rules are independent and preserve default dismissal',()=>{
 for(const [options,escape,outside] of [[{},false,false],[{closeOnEsc:false},true,false],[{closeOnBackdrop:false},false,true],[{backdrop:false},false,true],[{backdrop:false,closeOnEsc:false},true,true]] as const){const preventDefault=vi.fn(),policy=overlayPolicy(options);policy.onEscapeKeyDown({preventDefault});expect(preventDefault).toHaveBeenCalledTimes(escape?1:0);preventDefault.mockClear();policy.onInteractOutside({preventDefault});expect(preventDefault).toHaveBeenCalledTimes(outside?1:0);}
});});
