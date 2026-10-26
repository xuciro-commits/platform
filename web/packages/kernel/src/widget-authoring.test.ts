import {expect,expectTypeOf,test} from 'vitest';
import type * as Api from './gen/host';
import type {PageWidgetConfiguration,PageWidgetConfigurationFor} from './widget-authoring';

test('manifest author types narrow properties and ports while retained sections keep their original wire shape',()=>{
 expectTypeOf<Api.PageWidgetConfiguration>().toEqualTypeOf<PageWidgetConfiguration>();
 expectTypeOf<PageWidgetConfigurationFor<'heading'>>().toHaveProperty('headingLevel').toEqualTypeOf<'h1'|'h2'|'h3'|undefined>();
 expectTypeOf<PageWidgetConfigurationFor<'heading'>>().not.toHaveProperty('collectionVariable');
 expectTypeOf<PageWidgetConfigurationFor<'table'>>().toHaveProperty('collectionVariable').toEqualTypeOf<string|undefined>();
 expectTypeOf<PageWidgetConfigurationFor<'avatar-stack'>>().toHaveProperty('avatar');
 expectTypeOf<PageWidgetConfigurationFor<'table'>>().toHaveProperty('inlineEdit').toEqualTypeOf<Api.PageInlineEdit|undefined>();
 expectTypeOf<PageWidgetConfigurationFor<'range-input'>>().toHaveProperty('rangeInput').toEqualTypeOf<Api.PageRangeInput|undefined>();
 expectTypeOf<PageWidgetConfigurationFor<'heading'>>().not.toHaveProperty('rangeInput');
 expectTypeOf<PageWidgetConfigurationFor<'layered-graph'>>().toHaveProperty('composite').toEqualTypeOf<Api.PageComposite|undefined>();
 const retained: Api.Section={widget:'heading',headingLevel:'retained-profile-value'};
 expect(retained.headingLevel).toBe('retained-profile-value');
});
