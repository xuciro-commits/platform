import ts from "../web/packages/catalog/node_modules/typescript-syntax/lib/typescript.js";
import { readFile, readdir } from "node:fs/promises";
import path from "node:path";

export const owners = ["ui", "app", "build"];
export const ownerPackage = (owner) => owner.startsWith("@") ? owner : owner === "build" ? "@pkg/build" : `@platform/${owner}`;
export const ownerDirectory = (owner) => `web/packages/${owner}`;
export const dictionaryFile = (owner) => `${ownerDirectory(owner)}/src/${owner === "ui" ? "i18n/zh-CN.ts" : "i18n.ts"}`;

/** ADR-0049: configuration belongs to applications; domain pages cannot enter
 * the generic presentation owner by changing their import path. */
export function professionalViewGate({source, owner="@platform/app", configurations=[]}) {
  const errors=[];
  const domain=source.match(/\b(?:RoutingFlow|mes|sfc|workCenter|operation|equipment|warehouse|bed|ncCodes)\b/i);
  if(domain)errors.push(`Professional view contains domain vocabulary: ${domain[0]}`);
  if(owner==="@platform/ui"){
    const authors=new Set(configurations.map(c=>c.author).filter(Boolean));
    const apps=new Set(configurations.map(c=>c.application).filter(Boolean));
    if(authors.size<2||apps.size<2)errors.push("A new UI component needs configurations in two applications by two independent authors");
  }
  return errors;
}

export function syntax(file, text) {
  return ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
}

const exported = (node) => node.modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword);

/** Read public exports without resolving types or executing component code. */
export async function publicExports(file, read = (file) => readFile(file, "utf8"), prefix = "", cache = new Map()) {
  if (cache.has(file)) return cache.get(file);
  const result = new Set();
  cache.set(file, result);
  const source = syntax(file, await read(file));
  for (const node of source.statements) {
    if (ts.isExportAssignment(node) || exported(node) && node.modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.DefaultKeyword)) {
      result.add(`${prefix}default`);
    } else if (ts.isExportDeclaration(node) && !node.isTypeOnly) {
      const clause = node.exportClause;
      if (clause && ts.isNamedExports(clause)) {
        for (const item of clause.elements) if (!item.isTypeOnly) result.add(`${prefix}${item.name.text}`);
      } else if (node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier) && node.moduleSpecifier.text.startsWith(".")) {
        const base = path.resolve(path.dirname(file), node.moduleSpecifier.text);
        let target;
        for (const candidate of [base + ".ts", base + ".tsx", path.join(base, "index.ts")]) {
          try { await read(candidate); target = candidate; break; } catch { /* try next extension */ }
        }
        if (!target) throw new Error(`Unresolved public export: ${file} → ${node.moduleSpecifier.text}`);
        const namespace = clause && ts.isNamespaceExport(clause) ? `${clause.name.text}.` : "";
        for (const name of await publicExports(target, read, "", cache)) result.add(`${prefix}${namespace}${name}`);
      }
    } else if (exported(node) && !node.modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.DefaultKeyword)) {
      if (ts.isVariableStatement(node)) {
        for (const declaration of node.declarationList.declarations) if (ts.isIdentifier(declaration.name)) result.add(`${prefix}${declaration.name.text}`);
      } else if ((ts.isFunctionDeclaration(node) || ts.isClassDeclaration(node) || ts.isEnumDeclaration(node)) && node.name) result.add(`${prefix}${node.name.text}`);
    }
  }
  return result;
}

export function publicTypes(file, text) {
  const names = new Set();
  for (const node of syntax(file, text).statements) {
    if (ts.isExportDeclaration(node) && node.exportClause && ts.isNamedExports(node.exportClause)) {
      for (const item of node.exportClause.elements) if (node.isTypeOnly || item.isTypeOnly) names.add(item.name.text);
    } else if (exported(node) && (ts.isTypeAliasDeclaration(node) || ts.isInterfaceDeclaration(node))) names.add(node.name.text);
  }
  return names;
}

/** Owner dictionaries are the only source of localized catalog copy. */
export function readDictionary(file, text) {
  const dictionary = {};
  function visit(node) {
    if (ts.isPropertyAssignment(node) && ts.isStringLiteral(node.name) && ts.isStringLiteralLike(node.initializer)) {
      dictionary[node.name.text] = node.initializer.text;
    }
    ts.forEachChild(node, visit);
  }
  visit(syntax(file, text));
  return dictionary;
}

const placeholders = (text) => [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort().join(",");

export function validateOwner(owner, entries, api, exports, examples, dictionary, widgets, types = new Set()) {
  const errors = [];
  const classified = new Set(api);
  for (const name of api) {
    if (!exports.has(name) && !types.has(name)) errors.push(`${owner}: unknown API reference ${name}`);
    if (/^[A-Z]/.test(name) && !name.endsWith("Context") && !types.has(name)) errors.push(`${owner}: visual export ${name} cannot be hidden in API helpers`);
    if (name.startsWith("field.")) errors.push(`${owner}: field capability ${name} requires an illustrated asset`);
  }
  for (const entry of entries) {
    const fail = (message) => errors.push(`${entry.id ?? owner}: ${message}`);
    if (entry.owner !== ownerPackage(owner) || !/^[a-z][a-z0-9-]*\/[a-z0-9-]+$/.test(entry.id ?? "")) fail("invalid owner or qualified ID");
    if (![0, 1, 2, 3, 4, 5].includes(entry.layer) || !["specification", "api", "recommendation", "example"].includes(entry.authority) ||
      !["recommended", "experimental", "deprecated"].includes(entry.maturity) || !["platform", "domain"].includes(entry.scope)) fail("invalid classification");
    if (!entry.uses?.length || entry.uses.some((use) => !["code", "widget", "block", "template", "reference"].includes(use))) fail("invalid supported uses");
    if (!Array.isArray(entry.tags) || !entry.source) fail("missing tags or source");
    const text = [entry.name, entry.summary, ...(entry.constraints ?? []), ...(entry.states ?? [])];
    for (const source of text) {
      if (!source || source.length > 1000) fail("missing or oversized display text");
      else if (!dictionary[source]) fail(`missing zh-CN text: ${source}`);
      else if (placeholders(source) !== placeholders(dictionary[source])) fail(`translation placeholder mismatch: ${source}`);
    }
    if (entry.example && !examples.has(entry.example)) fail(`missing example export ${entry.example}`);
    if (entry.type && /^[A-Za-z_$][\w$]*$/.test(entry.type) && !types.has(entry.type) && !exports.has(entry.type)) fail(`unknown public type ${entry.type}`);
    if (entry.exports?.length && !entry.example) fail("public visual capabilities require a real example");
    for (const name of entry.exports ?? []) {
      if (!exports.has(name)) fail(`unknown public export ${name}`);
      if (classified.has(name)) fail(`public export ${name} classified more than once`);
      classified.add(name);
    }
    for (const widget of entry.widgets ?? []) if (!widgets.has(widget)) fail(`unknown Page widget ${widget}`);
    if (entry.uses.includes("widget") && !entry.widgets?.length) fail("widget use requires an original Widget reference");
    if (entry.uses.includes("template") && !entry.template) fail("template use requires a Studio template ID");
  }
  for (const name of exports) if (!classified.has(name)) errors.push(`${owner}: public export ${name} is not registered`);
  return errors;
}

export async function sourceFiles(directory) {
  const output = [];
  async function walk(current) {
    for (const entry of await readdir(current, { withFileTypes: true })) {
      if (["node_modules", "dist", "gen"].includes(entry.name)) continue;
      const file = path.join(current, entry.name);
      if (entry.isDirectory()) await walk(file);
      else if (/\.[cm]?[jt]sx?$/.test(entry.name) && !/\.(test|spec)\./.test(entry.name)) output.push(file);
    }
  }
  await walk(directory);
  return output.sort();
}

const withoutExtension = (file) => file.replace(/\.[cm]?[jt]sx?$/, "");

/** Conservative import references, not an exhaustive runtime call graph. */
export function inspectImports(file, text, packages, entries) {
  const errors = [], consumers = new Set();
  const source = syntax(file, text);
  function visit(node) {
    const specifier = ts.isImportDeclaration(node) || ts.isExportDeclaration(node) ? node.moduleSpecifier :
      ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword ? node.arguments[0] : undefined;
    if (specifier && ts.isStringLiteral(specifier)) {
      const module = specifier.text;
      for (const [owner, pkg] of packages) {
        if (module.startsWith(pkg.name + "/") && !("./" + module.slice(pkg.name.length + 1) in pkg.exports)) errors.push(`${file}: private import ${module}`);
        if (module.startsWith(".")) {
          const target = path.posix.normalize(path.posix.join(path.posix.dirname(file), module));
          const directory = ownerDirectory(owner);
          if (target.startsWith(directory + "/src/") && !file.startsWith(directory + "/")) errors.push(`${file}: private relative import ${module}`);
        }
      }
      if (ts.isImportDeclaration(node) && node.importClause?.namedBindings && ts.isNamedImports(node.importClause.namedBindings)) {
        const names = new Set(node.importClause.namedBindings.elements.filter((item) => !item.isTypeOnly).map((item) => item.propertyName?.text ?? item.name.text));
        const relative = module.startsWith(".") ? withoutExtension(path.posix.normalize(path.posix.join(path.posix.dirname(file), module))) : undefined;
        for (const entry of entries) {
          if (!entry.exports?.some((name) => names.has(name.split(".")[0]))) continue;
          if (relative ? withoutExtension(entry.source) === relative : entry.owner === module) consumers.add(entry.id);
        }
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(source);
  return { errors, consumers: [...consumers] };
}

/**
 * Bounded, syntax-only evidence for one catalog asset. No type guesses or JSX
 * traversal outside the exported component. Full semantic typing stays with TS.
 */
export function extractComponentApi(entry, text, allEntries = []) {
  const source = syntax(entry.source, text);
  const declarations = new Map(source.statements.filter((node) =>
    (ts.isTypeAliasDeclaration(node) || ts.isInterfaceDeclaration(node)) && node.name).map((node) => [node.name.text, node]));
  const functions = new Map();
  const imports = new Map();
  const cva = new Map();

  for (const stmt of source.statements) {
    if (ts.isFunctionDeclaration(stmt) && stmt.name) functions.set(stmt.name.text, stmt);
    if (ts.isVariableStatement(stmt)) for (const decl of stmt.declarationList.declarations) {
      if (!ts.isIdentifier(decl.name)) continue;
      if (decl.initializer && (ts.isArrowFunction(decl.initializer) || ts.isFunctionExpression(decl.initializer))) functions.set(decl.name.text, decl.initializer);
      if (decl.initializer && ts.isCallExpression(decl.initializer) && decl.initializer.expression.getText(source) === "cva") cva.set(decl.name.text, decl.initializer);
    }
    if (ts.isImportDeclaration(stmt) && ts.isStringLiteral(stmt.moduleSpecifier) && stmt.importClause) {
      const module = stmt.moduleSpecifier.text;
      if (stmt.importClause.name) imports.set(stmt.importClause.name.text, { module, symbol: "default" });
      const bindings = stmt.importClause.namedBindings;
      if (ts.isNamedImports(bindings)) for (const item of bindings.elements) {
        if (!item.isTypeOnly) imports.set(item.name.text, { module, symbol: item.propertyName?.text ?? item.name.text });
      }
    }
  }

  // The first visual export is the owner-designated primary component. Other
  // public exports stay listed separately; don't combine their Props into one.
  const component = entry.exports?.find((name) => functions.has(name.split(".")[0]))?.split(".")[0];
  const target = component && functions.get(component);
  const param = target?.parameters?.[0];
  const defaults = new Map();
  if (param && ts.isObjectBindingPattern(param.name)) for (const element of param.name.elements) {
    if (!element.dotDotDotToken && ts.isIdentifier(element.name) && element.initializer) {
      defaults.set(element.name.text, element.initializer.getText(source));
    }
  }

  const props = new Map();
  const usedTypes = new Set();
  const seenTypes = new Set();
  const markTypes = (node) => {
    if (ts.isTypeReferenceNode(node) && ts.isIdentifier(node.typeName)) usedTypes.add(node.typeName.text);
    ts.forEachChild(node, markTypes);
  };

  const addMembers = (members) => {
    for (const member of members) {
      if (!member.name || !ts.isIdentifier(member.name)) continue;
      const name = member.name.text;
      const type = member.type?.getText(source).replace(/\s+/g, " ").trim() ?? "unknown";
      if (member.type) markTypes(member.type);
      const isCallback = ts.isMethodSignature(member) || !!member.type && ts.isFunctionTypeNode(member.type) ||
        /^on[A-Z]/.test(name) && /=>|EventHandler/.test(type);
      const isSlot = /\bReactNode\b|\bReactElement\b|\bJSX\.Element\b/.test(type) || name === "children";
      props.set(name, {
        name, type, optional: !!member.questionToken, isCallback, isSlot,
        ...(defaults.has(name) ? { defaultValue: defaults.get(name) } : {}),
      });
    }
  };

  const visitType = (node) => {
    if (!node || seenTypes.has(node)) return;
    seenTypes.add(node);
    if (ts.isTypeLiteralNode(node)) addMembers(node.members);
    else if (ts.isInterfaceDeclaration(node)) {
      addMembers(node.members);
      for (const parent of node.heritageClauses ?? []) for (const ref of parent.types) {
        if (ts.isIdentifier(ref.expression)) visitType(declarations.get(ref.expression.text));
      }
    } else if (ts.isTypeAliasDeclaration(node)) visitType(node.type);
    else if (ts.isIntersectionTypeNode(node)) node.types.forEach(visitType);
    else if (ts.isTypeReferenceNode(node) && ts.isIdentifier(node.typeName)) {
      usedTypes.add(node.typeName.text);
      visitType(declarations.get(node.typeName.text));
    }
    // Imported/generic/utility types cannot be resolved with syntax alone.
  };

  if (param?.type) visitType(param.type);

  // A CVA variable is relevant only if the target's declared Props refer to it.
  const signature = param?.type?.getText(source) ?? "";
  const referencedTypes = [...seenTypes].map((node) => node.getText(source)).join(" ");
  for (const [name, call] of cva) {
    if (!new RegExp(`\\btypeof\\s+${name}\\b`).test(`${signature} ${referencedTypes}`)) continue;
    const config = call.arguments[1];
    if (!config || !ts.isObjectLiteralExpression(config)) continue;
    const variants = config.properties.find((item) => item.name?.getText(source) === "variants")?.initializer;
    if (!variants || !ts.isObjectLiteralExpression(variants)) continue;
    for (const variant of variants.properties) {
      if (!ts.isPropertyAssignment(variant) || !ts.isObjectLiteralExpression(variant.initializer)) continue;
      const name = variant.name.getText(source).replace(/^['"]|['"]$/g, "");
      if (props.has(name)) continue;
      const keys = variant.initializer.properties.map((item) => item.name?.getText(source)).filter(Boolean);
      props.set(name, { name, type: keys.map((key) => JSON.stringify(key.replace(/^[\'"]|[\'"]$/g, ""))).join(" | "), optional: true, isCallback: false, isSlot: false });
    }
  }

  // An explicit default proves a binding can be omitted, but does not prove its type.
  // Inherited HTML Props and rest bindings stay unresolved rather than guessed.
  if (param && ts.isObjectBindingPattern(param.name)) for (const item of param.name.elements) {
    if (!item.dotDotDotToken && ts.isIdentifier(item.name) && item.initializer && !props.has(item.name.text)) {
      const name = item.name.text;
      props.set(name, {
        name, type: "unknown", optional: true, isCallback: false, isSlot: false,
        defaultValue: item.initializer.getText(source)
      });
    }
  }

  const relatedTypes = [...usedTypes, entry.type].filter((name) => name && declarations.has(name))
    .filter((name, i, names) => names.indexOf(name) === i)
    .map((name) => {
      const node = declarations.get(name);
      return { name, kind: ts.isInterfaceDeclaration(node) ? "interface" : "type", signature: node.getText(source) };
    });

  const composed = new Set();
  if (target) {
    const visitJsx = (node) => {
      if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
        if (ts.isIdentifier(node.tagName)) {
          const imp = imports.get(node.tagName.text);
          if (imp) {
            const targetSource = imp.module.startsWith(".") ? withoutExtension(path.posix.normalize(path.posix.join(path.posix.dirname(entry.source), imp.module))) : undefined;
            for (const other of allEntries) {
              if (other.id === entry.id || !other.exports?.some((name) => name.split(".")[0] === imp.symbol)) continue;
              if (targetSource ? withoutExtension(other.source) === targetSource : imp.module === other.owner) composed.add(other.id);
            }
          }
        }
      }
      ts.forEachChild(node, visitJsx);
    };
    visitJsx(target);
  }

  const items = [...props.values()];
  return {
    props: items.length ? items : undefined,
    slots: items.some((prop) => prop.isSlot) ? items.filter((prop) => prop.isSlot).map(({ name, type, optional }) => ({ name, type, optional })) : undefined,
    composed: composed.size ? [...composed].sort() : undefined,
    relatedTypes: relatedTypes.length ? relatedTypes : undefined,
  };
}
