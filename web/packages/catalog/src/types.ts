export type CatalogEntry = {
  id: string;
  owner: string;
  name: string;
  summary: string;
  layer: 0 | 1 | 2 | 3 | 4 | 5;
  authority: "specification" | "api" | "recommendation" | "example";
  maturity: "recommended" | "experimental" | "deprecated";
  scope: "platform" | "domain";
  uses: ("code" | "widget" | "block" | "template" | "reference")[];
  tags: string[];
  source: string;
  exports?: string[];
  type?: string;
  example?: string;
  snippet?: string;
  dependencies?: string[];
  constraints?: string[];
  states?: string[];
  widgets?: string[];
  template?: string;
  props?: CatalogProp[];
  slots?: CatalogSlot[];
  composed?: string[];
  relatedTypes?: CatalogRelatedType[];
};

type CatalogProp = {
  name: string;
  type: string;
  optional: boolean;
  defaultValue?: string;
  comment?: string;
  isCallback?: boolean;
  isSlot?: boolean;
};

type CatalogSlot = {
  name: string;
  type: string;
  optional?: boolean;
  description?: string;
};

type CatalogRelatedType = {
  name: string;
  kind?: "interface" | "type" | "enum";
  signature?: string;
};

export type CatalogIndex = {
  revision: string;
  entries: CatalogEntry[];
  translations: Record<string, Record<string, string>>;
  consumers?: Record<string, string[]>;
  api?: Record<string, CatalogApi>;
};

export type CatalogApi = { source: string; symbols: string[]; types: string[] };

export type CatalogQuery = {
  query?: string;
  layer?: number;
  owner?: string;
  use?: CatalogEntry["uses"][number];
  language?: string;
  scope?: CatalogEntry["scope"];
  maturity?: CatalogEntry["maturity"];
  offset?: number;
  limit?: number;
};
