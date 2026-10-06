import {t} from '@platform/ui';
import {pageUIManifest} from '@platform/kernel';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {InputProps} from './Input';
import type {BooleanInputRenderer} from './BooleanInput';
import type {ChoiceInputRenderer} from './ChoiceInput';
import type {DateInputRenderer} from './DateInput';
import type {RangeRenderer} from './Range';

const define=defineWidgetPlugin<WidgetBindingContext>();
export const inputPlugins={
 input:define('input',1,({section,value,onValue,enabled,numeric,valueError,inputScopes}):InputProps=>({kind:section.inputKind,title:section.title||t(section.inputKind==='scan'?'Scan a code':section.inputKind==='search'?'Search records':'Text input'),value,onChange:onValue,enabled,numeric,error:valueError,scopes:inputScopes,maxBytes:pageUIManifest.runtime.decimal.maxBytes}),()=>import('./Input').then(m=>m.InputRenderer)),
 'boolean-input':define('boolean-input',1,({section,booleanInput,onBoolean,enabled}):Parameters<typeof BooleanInputRenderer>[0]=>({value:booleanInput,label:section.booleanLabel,variant:section.booleanVariant,title:section.title||t('Boolean switch'),enabled,onChange:onBoolean}),()=>import('./BooleanInput').then(m=>m.BooleanInputRenderer)),
 'choice-input':define('choice-input',1,({section,choiceValue,choiceSetValue,onChoice,onChoiceSet,enabled}):Parameters<typeof ChoiceInputRenderer>[0]=>({setValue:choiceSetValue,onSet:onChoiceSet,value:choiceValue,fields:section.choiceInput,title:section.title||t('Choice input'),enabled,onChange:onChoice}),()=>import('./ChoiceInput').then(m=>m.ChoiceInputRenderer)),
 'date-input':define('date-input',1,({section,dateValue,onDate,enabled}):Parameters<typeof DateInputRenderer>[0]=>({value:dateValue,kind:section.dateKind,offset:section.dateOffset,label:section.dateLabel,title:section.title||t('Date input'),enabled,onChange:onDate}),()=>import('./DateInput').then(m=>m.DateInputRenderer)),
 'range-input':define('range-input',1,({section,rangeLower,rangeUpper,onRange}):Parameters<typeof RangeRenderer>[0]=>({lower:rangeLower,upper:rangeUpper,fields:section.rangeInput,title:section.title||t('Range input'),onChange:onRange}),()=>import('./Range').then(m=>m.RangeRenderer)),
};
