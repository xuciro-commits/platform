import type {pageUIManifest, Section} from './gen/host';

type Contract = typeof pageUIManifest.widgets[number];
export type PageWidgetID = Contract['componentID'];
type RequiredKeys<S> = S extends {required: readonly (infer K)[]} ? K : never;
type SchemaObject<P, R> = {[K in keyof P as K extends R ? K : never]: SchemaValue<P[K]>}
  & {[K in keyof P as K extends R ? never : K]?: SchemaValue<P[K]>};
type SchemaValue<S> = S extends {enum: readonly (infer E)[]} ? E
  : S extends {type: 'object'; properties: infer P} ? SchemaObject<P, RequiredKeys<S>>
  : S extends {type: 'array'; items: infer I} ? SchemaValue<I>[]
  : S extends {type: 'boolean'} ? boolean
  : S extends {type: 'number' | 'integer'} ? number
  : S extends {type: 'string'} ? string : never;
type PortPath<P extends string> = P extends `${infer Head}.${infer Tail}` ? {[K in Head]?: PortPath<Tail>} : {[K in P]?: string};
type UnionIntersection<U> = (U extends unknown ? (item: U) => void : never) extends ((item: infer I) => void) ? I : never;
type Ports<C extends Contract> = C extends {inputPorts: infer I extends readonly unknown[]; outputPorts: infer O extends readonly unknown[]}
  ? UnionIntersection<(I[number] | O[number]) extends infer P ? P extends {bindingField: infer F extends string} ? F extends 'enabledWhen' ? {} : PortPath<F> : never : never> : {};
type ConfigurationFields<C extends Contract> = C extends {configurationFields: readonly (infer K)[]}
  ? Pick<Section, Extract<K, keyof Section>> : {};

/** The same manifest generates the source author's discriminated presentation
 * and port types. Business models and actions keep their original declarations;
 * the retained flat Section remains the installed/history wire representation. */
export type PageWidgetConfiguration = {
  [C in Contract as C['componentID']]: {widget: C['componentID']; configVersion: C['configVersion']}
    & {id?: string} & SchemaValue<C['propsSchema']> & Ports<C> & ConfigurationFields<C>
}[PageWidgetID];
export type PageWidgetConfigurationFor<K extends PageWidgetID> = Extract<PageWidgetConfiguration, {widget: K}>;
