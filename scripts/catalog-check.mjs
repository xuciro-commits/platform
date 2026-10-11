import ts from "../web/packages/catalog/node_modules/typescript-syntax/lib/typescript.js";
import { readFile, readdir } from "node:fs/promises";
import path from "node:path";

export const owners = ["ui", "app", "build"];
export const ownerPackage = (owner) => owner.startsWith("@") ? owner : owner === "build" ? "@pkg/build" : `@platform/${owner}`;
export const ownerDirectory = (owner) => `web/packages/${owner}`;
export const dictionaryFile = (owner) => `${ownerDirectory(owner)}/src/${owner === "ui" ? "i18n/zh-CN.ts" : "i18n.ts"}`;

export function syntax(file, text) {
  return ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
}

function exported(node) {
  return node.modifiers?.some((modifier) => modifier.kind === ts.SyntaxKind.ExportKeyword);
}

/** Syntax inspection only. No program, type inference, JSX execution or doc generator. */
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
          try { await read(candidate); target = candidate; break; } catch { /* try the next source extension */ }
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

/** Existing register("zh-CN", {...}) and UI dictionaries remain the sole text owner. */
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

export function inspectImports(file, text, packages, entries) {
  const errors = [], consumers = [];
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
        if (module === pkg.name && ts.isImportDeclaration(node) && node.importClause?.namedBindings) {
          const bindings = node.importClause.namedBindings;
          if (ts.isNamedImports(bindings)) {
            const names = new Set(bindings.elements.filter((item) => !item.isTypeOnly).map((item) => item.propertyName?.text ?? item.name.text));
            for (const entry of entries.filter((entry) => entry.owner === ownerPackage(owner))) {
              if (entry.exports?.some((name) => names.has(name.split(".")[0]))) consumers.push(entry.id);
            }
          }
        }
      }

      // Check relative imports with exact normalized file target matching (no fuzzy basename match)
      if (module.startsWith(".") && ts.isImportDeclaration(node) && node.importClause?.namedBindings) {
        const target = path.posix.normalize(path.posix.join(path.posix.dirname(file), module));
        const targetNorm = target.replace(/\.[cm]?[jt]sx?$/, "");
        const bindings = node.importClause.namedBindings;
        if (ts.isNamedImports(bindings)) {
          const names = new Set(bindings.elements.filter((item) => !item.isTypeOnly).map((item) => item.propertyName?.text ?? item.name.text));
          for (const entry of entries) {
            const entrySourceNorm = entry.source.replace(/\.[cm]?[jt]sx?$/, "");
            if (targetNorm === entrySourceNorm) {
              if (entry.exports?.some((name) => names.has(name.split(".")[0]))) consumers.push(entry.id);
            }
          }
        }
      }
    }
    ts.forEachChild(node, visit);
  }
  visit(source);
  return { errors, consumers: [...new Set(consumers)] };
}

/** Extracts structured API details, props, slots, and internal JSX composition from AST. */
export function extractComponentApi(entry, text, allEntries = []) {
  const source = syntax(entry.source, text);
  const props = [];
  const slots = [];
  const composed = new Set();
  const relatedTypes = [];

  // 1. Build import map to verify exact symbol origins
  const importsByLocalName = new Map();
  for (const stmt of source.statements) {
    if (ts.isImportDeclaration(stmt) && stmt.moduleSpecifier && ts.isStringLiteral(stmt.moduleSpecifier)) {
      const module = stmt.moduleSpecifier.text;
      const clause = stmt.importClause;
      if (clause) {
        if (clause.name) {
          importsByLocalName.set(clause.name.text, { module, importedName: "default" });
        }
        if (clause.namedBindings) {
          if (ts.isNamedImports(clause.namedBindings)) {
            for (const el of clause.namedBindings.elements) {
              const localName = el.name.text;
              const importedName = el.propertyName ? el.propertyName.text : localName;
              importsByLocalName.set(localName, { module, importedName });
            }
          } else if (ts.isNamespaceImport(clause.namedBindings)) {
            importsByLocalName.set(clause.namedBindings.name.text, { module, importedName: "*" });
          }
        }
      }
    }
  }

  // 2. Identify the target component(s) belonging to this catalog entry
  const entryExports = entry.exports || [];
  const idSlug = (entry.id.split("/")[1] || "").toLowerCase().replace(/[^a-z0-9]/g, "");
  const entryNameNorm = (entry.name || "").toLowerCase().replace(/[^a-z0-9]/g, "");

  let primaryExport = entryExports.find((exp) => {
    const norm = exp.toLowerCase().replace(/[^a-z0-9]/g, "");
    return norm === idSlug || norm === entryNameNorm;
  });

  if (!primaryExport && entry.type) {
    primaryExport = entryExports.find((exp) => entry.type.startsWith(exp));
  }
  if (!primaryExport) {
    primaryExport = entryExports.find((exp) => /^[A-Z]/.test(exp)) || entryExports[0];
  }

  const targetExports = primaryExport ? [primaryExport] : entryExports.filter((exp) => /^[A-Z]/.test(exp));

  function extractPropFromMember(member, sourceFile, defaultValues = new Map()) {
    if (!member.name) return null;
    const name = member.name.getText(sourceFile);
    if (!name || name.startsWith("[")) return null;
    const optional = !!member.questionToken;
    let typeText = "unknown";
    if (member.type) {
      typeText = member.type.getText(sourceFile).replace(/\s+/g, " ").trim();
    }
    const isCallback = (name.startsWith("on") && name.length > 2 && /^[A-Z]/.test(name[2])) ||
      typeText.includes("=>") || typeText.includes("Function") || typeText.includes("EventHandler");
    const isSlot = name === "children" || name.toLowerCase().includes("slot") ||
      name === "actions" || name === "toolbar" || name === "dock" || name === "header" ||
      name === "footer" || typeText.includes("ReactNode") || typeText.includes("ReactElement") ||
      typeText.includes("JSX.Element");

    const defaultValue = defaultValues.get(name);
    return { name, type: typeText, optional, isCallback, isSlot, defaultValue };
  }

  function extractDefaults(param, sourceFile) {
    const defaults = new Map();
    if (param && ts.isObjectBindingPattern(param.name)) {
      for (const element of param.name.elements) {
        if (element.name && element.initializer) {
          defaults.set(element.name.getText(sourceFile), element.initializer.getText(sourceFile).trim());
        }
      }
    }
    return defaults;
  }

  const propMap = new Map();
  const defaultValues = new Map();
  const targetComponentNodes = [];

  // 3. Find component function/variable nodes belonging strictly to target component(s)
  for (const targetComp of targetExports) {
    for (const stmt of source.statements) {
      if (ts.isFunctionDeclaration(stmt) && stmt.name?.text === targetComp) {
        targetComponentNodes.push(stmt);
      } else if (ts.isVariableStatement(stmt)) {
        for (const decl of stmt.declarationList.declarations) {
          if (decl.name?.getText(source) === targetComp && decl.initializer) {
            if (ts.isArrowFunction(decl.initializer) || ts.isFunctionExpression(decl.initializer)) {
              targetComponentNodes.push(decl.initializer);
            }
          }
        }
      }
    }
  }

  // 4. Collect default parameter values from target component functions
  for (const fnNode of targetComponentNodes) {
    if (fnNode.parameters?.length > 0) {
      const firstParam = fnNode.parameters[0];
      const paramDefaults = extractDefaults(firstParam, source);
      for (const [k, v] of paramDefaults) defaultValues.set(k, v);
    }
  }

  // 5. Build candidate type names to search
  const candidateTypeNames = new Set();
  if (entry.type) candidateTypeNames.add(entry.type);
  for (const targetComp of targetExports) {
    candidateTypeNames.add(`${targetComp}Props`);
    candidateTypeNames.add(`${targetComp}Options`);
    candidateTypeNames.add(`${targetComp}Config`);
  }
  candidateTypeNames.add("Props");

  for (const fnNode of targetComponentNodes) {
    if (fnNode.parameters?.length > 0) {
      const firstParam = fnNode.parameters[0];
      if (firstParam.type) {
        if (ts.isTypeReferenceNode(firstParam.type)) {
          candidateTypeNames.add(firstParam.type.typeName.getText(source));
        } else if (ts.isIntersectionTypeNode(firstParam.type)) {
          for (const sub of firstParam.type.types) {
            if (ts.isTypeReferenceNode(sub)) candidateTypeNames.add(sub.typeName.getText(source));
          }
        }
      }
    }
  }

  // 6. Extract props from inline type literals on parameters
  for (const fnNode of targetComponentNodes) {
    if (fnNode.parameters?.length > 0) {
      const firstParam = fnNode.parameters[0];
      if (firstParam.type && ts.isTypeLiteralNode(firstParam.type)) {
        for (const m of firstParam.type.members) {
          const prop = extractPropFromMember(m, source, defaultValues);
          if (prop && !propMap.has(prop.name)) {
            propMap.set(prop.name, prop);
          }
        }
      }
    }
  }

  // 7. Resolve interface or type alias declarations matching candidate types (REAL TYPES FIRST)
  for (const stmt of source.statements) {
    const stmtName = stmt.name?.text;
    if (stmtName && candidateTypeNames.has(stmtName)) {
      if (ts.isInterfaceDeclaration(stmt)) {
        for (const m of stmt.members) {
          const prop = extractPropFromMember(m, source, defaultValues);
          if (prop && !propMap.has(prop.name)) {
            propMap.set(prop.name, prop);
          }
        }
      } else if (ts.isTypeAliasDeclaration(stmt)) {
        if (ts.isTypeLiteralNode(stmt.type)) {
          for (const m of stmt.type.members) {
            const prop = extractPropFromMember(m, source, defaultValues);
            if (prop && !propMap.has(prop.name)) {
              propMap.set(prop.name, prop);
            }
          }
        } else if (ts.isIntersectionTypeNode(stmt.type)) {
          for (const subType of stmt.type.types) {
            if (ts.isTypeLiteralNode(subType)) {
              for (const m of subType.members) {
                const prop = extractPropFromMember(m, source, defaultValues);
                if (prop && !propMap.has(prop.name)) {
                  propMap.set(prop.name, prop);
                }
              }
            }
          }
        }
      }
    }
  }

  // 8. CVA variants scanning (e.g. Button, RetroButton, StatusTag)
  for (const stmt of source.statements) {
    if (ts.isVariableStatement(stmt)) {
      for (const decl of stmt.declarationList.declarations) {
        if (decl.initializer && ts.isCallExpression(decl.initializer) && decl.initializer.expression.getText(source) === "cva") {
          const config = decl.initializer.arguments?.[1];
          if (config && ts.isObjectLiteralExpression(config)) {
            for (const prop of config.properties) {
              if (prop.name?.getText(source) === "variants" && prop.initializer && ts.isObjectLiteralExpression(prop.initializer)) {
                for (const v of prop.initializer.properties) {
                  const vName = v.name?.getText(source);
                  if (vName && v.initializer && ts.isObjectLiteralExpression(v.initializer)) {
                    const options = v.initializer.properties.map((p) => `"${p.name?.getText(source)}"`).join(" | ");
                    if (!propMap.has(vName)) {
                      propMap.set(vName, { name: vName, type: options || "string", optional: true, isCallback: false, isSlot: false });
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }

  // 9. Destructured parameters fallback (DO NOT GUESS TYPES, only use initializer or "unknown")
  for (const fnNode of targetComponentNodes) {
    if (fnNode.parameters?.length > 0) {
      const firstParam = fnNode.parameters[0];
      if (firstParam && ts.isObjectBindingPattern(firstParam.name)) {
        for (const element of firstParam.name.elements) {
          const pName = element.name?.getText(source);
          if (pName && !pName.startsWith("...") && pName !== "props" && !propMap.has(pName)) {
            let actualType = "unknown";
            if (element.initializer) {
              if (ts.isStringLiteral(element.initializer)) actualType = "string";
              else if (ts.isNumericLiteral(element.initializer)) actualType = "number";
              else if (element.initializer.kind === ts.SyntaxKind.TrueKeyword || element.initializer.kind === ts.SyntaxKind.FalseKeyword) actualType = "boolean";
            }
            const isCallback = pName.startsWith("on") && pName.length > 2 && /^[A-Z]/.test(pName[2]);
            const isSlot = pName === "children" || pName.toLowerCase().includes("slot");
            propMap.set(pName, {
              name: pName,
              type: actualType,
              optional: true,
              isCallback,
              isSlot,
              defaultValue: defaultValues.get(pName),
            });
          }
        }
      }
    }
  }

  // 10. Extract Related Types (keep full signature without truncation)
  for (const stmt of source.statements) {
    if (exported(stmt) && (ts.isTypeAliasDeclaration(stmt) || ts.isInterfaceDeclaration(stmt) || ts.isEnumDeclaration(stmt))) {
      const name = stmt.name.text;
      relatedTypes.push({
        name,
        kind: ts.isInterfaceDeclaration(stmt) ? "interface" : ts.isEnumDeclaration(stmt) ? "enum" : "type",
        signature: stmt.getText(source).trim(),
      });
    }
  }

  // 11. Populate props and slots (with explicit optional boolean)
  for (const prop of propMap.values()) {
    props.push(prop);
    if (prop.isSlot) {
      slots.push({ name: prop.name, type: prop.type, optional: prop.optional });
    }
  }

  // 12. Scoped JSX composition with verified import origin
  function visitJsxNode(node) {
    if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
      const tagText = node.tagName.getText(source);
      if (/^[A-Z]/.test(tagText)) {
        const tagBase = tagText.split(".")[0];
        if (importsByLocalName.has(tagBase)) {
          const { module, importedName } = importsByLocalName.get(tagBase);
          if (module.startsWith("@platform/") || module.startsWith("@pkg/")) {
            for (const other of allEntries) {
              if (other.id !== entry.id && other.owner === module) {
                if (other.exports?.some((e) => e.split(".")[0] === importedName)) {
                  composed.add(other.id);
                }
              }
            }
          } else if (module.startsWith(".")) {
            const fromDir = path.posix.dirname(entry.source);
            const resolvedTarget = path.posix.normalize(path.posix.join(fromDir, module));
            for (const other of allEntries) {
              if (other.id !== entry.id) {
                const otherSourceNorm = other.source.replace(/\.[cm]?[jt]sx?$/, "");
                if (resolvedTarget === otherSourceNorm) {
                  if (other.exports?.some((e) => e.split(".")[0] === importedName)) {
                    composed.add(other.id);
                  }
                }
              }
            }
          }
          // External libraries (e.g. lucide-react, react, @tanstack/...) are ignored!
        } else {
          // Local declaration in the same file
          for (const other of allEntries) {
            if (other.id !== entry.id && other.source === entry.source) {
              if (other.exports?.some((e) => e.split(".")[0] === tagBase)) {
                composed.add(other.id);
              }
            }
          }
        }
      }
    }
    ts.forEachChild(node, visitJsxNode);
  }

  // Traverse JSX only inside target component nodes and local subcomponents called by them
  const localRenderedTags = new Set();
  function findLocalTags(node) {
    if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
      const tagText = node.tagName.getText(source);
      if (/^[A-Z]/.test(tagText)) {
        const tagBase = tagText.split(".")[0];
        if (!importsByLocalName.has(tagBase)) localRenderedTags.add(tagBase);
      }
    }
    ts.forEachChild(node, findLocalTags);
  }
  for (const node of targetComponentNodes) findLocalTags(node);

  for (const node of targetComponentNodes) visitJsxNode(node);

  for (const localTag of localRenderedTags) {
    for (const stmt of source.statements) {
      if ((ts.isFunctionDeclaration(stmt) && stmt.name?.text === localTag) ||
          (ts.isVariableStatement(stmt) && stmt.declarationList.declarations.some(d => d.name?.getText(source) === localTag))) {
        visitJsxNode(stmt);
      }
    }
  }

  return {
    props: props.length ? props : undefined,
    slots: slots.length ? slots : undefined,
    composed: composed.size ? [...composed].sort() : undefined,
    relatedTypes: relatedTypes.length ? relatedTypes : undefined,
  };
}
