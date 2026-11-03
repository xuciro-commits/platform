export const pageSafetyContract = {
  "format": 1,
  "section": {
    "bindingPaths": [
      "object",
      "model",
      "composite.bindings",
      "composite.value",
      "composite.acyclic",
      "query",
      "fields",
      "actions",
      "inputs",
      "inlineEdit",
      "facets",
      "selection",
      "collectionVariable",
      "recordVariable",
      "selectionVariable",
      "filterVariable",
      "filterSearchVariable",
      "selectionSetVariable",
      "parentSelection",
      "relation",
      "function",
      "operation",
      "group",
      "columnGroup",
      "measure",
      "timeStart",
      "timeEnd",
      "timeLabel",
      "timeGroup",
      "cardLabel",
      "actionDefaults",
      "actionTable.parameters",
      "collectionBuilder.fields"
    ],
    "resourcePaths": [
      "embedding.kind",
      "embedding.readOnly",
      "embedding.page",
      "embedding.contentVersion",
      "embedding.interfaceVersion",
      "embedding.inputs",
      "embedding.results",
      "externalFrame.url",
      "externalFrame.origin",
      "image.url",
      "scene.sampleAssetField",
      "scene.sampleTimeField",
      "scene.mappings[].source",
      "scene.mappings[].field",
      "observation.kind",
      "observation.timeField",
      "observation.signals[].field",
      "observation.metadata.plant",
      "observation.metadata.device",
      "observation.metadata.asset",
      "observation.metadata.status",
      "observation.assetObject",
      "observation.assetField",
      "observation.rowOutput",
      "observation.assetOutput",
      "observation.averageField",
      "graphExplorer.objects[].object",
      "graphExplorer.objects[].labelField",
      "graphExplorer.relations[].binding",
      "graphExplorer.outputs[].object",
      "graphExplorer.outputs[].variable",
      "vertexGraph.groups[].binding",
      "vertexGraph.groups[].direction",
      "vertexGraph.groups[].limit",
      "ai.kind",
      "ai.replyField",
      "ai.questionVariable",
      "recordLinks[].object",
      "recordLinks[].field",
      "resourceList.labelField",
      "resourceList.statusField",
      "assetDirectory.items[].asset",
      "avatar.labelField",
      "avatar.detailFields",
      "avatar.contextVariable",
      "avatar.contextCollectionVariable"
    ]
  },
  "scope": {
    "bindingPaths": [
      "valueVariable",
      "activeVariable",
      "visibleWhen",
      "enabledWhen",
      "loop.collection",
      "loop.itemVariable",
      "loop.limit"
    ],
    "overlayPaths": [
      "root",
      "openVariable"
    ],
    "documentPaths": [
      "variables",
      "queries",
      "interface",
      "events"
    ]
  },
  "scalarFieldPaths": [
    "group",
    "columnGroup",
    "timeStart",
    "timeEnd",
    "timeLabel",
    "timeGroup",
    "cardLabel",
    "summaryField",
    "measure",
    "recordGantt.startField",
    "recordGantt.endField",
    "recordGantt.titleField",
    "recordGantt.statusField",
    "recordCalendar.dateField",
    "recordCalendar.labelField",
    "recordEvents.timeField",
    "recordEvents.titleField",
    "recordEvents.severityField",
    "recordPicker.labelField",
    "recordCard.labelField",
    "recordComparison.labelField",
    "recordChart.xField",
    "recordChart.yField",
    "scatter.xField",
    "scatter.yField",
    "scatter.labelField",
    "scatter.colorField",
    "histogram.field",
    "leaderboard.valueField",
    "leaderboard.labelField",
    "statusTracker.field",
    "breadcrumb.labelField",
    "avatar.labelField",
    "sparkline.field"
  ],
  "retainedContract": {
    "uiProfile": "platform.page.v2.112",
    "widgets": [
      {
        "componentID": "inline-action",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.29",
        "selectionMode": "read",
        "bindingKinds": [
          "object",
          "actions",
          "selection",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.5"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "actionDefaults",
          "actions",
          "model",
          "object",
          "selection"
        ]
      },
      {
        "componentID": "table",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "write",
        "bindingKinds": [
          "object",
          "fields",
          "query",
          "relation",
          "selection",
          "collection-variable",
          "selection-variable",
          "filter-variable",
          "selection-set-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.10",
            "scopeRule": "local"
          },
          {
            "id": "filter",
            "bindingField": "filterVariable",
            "type": "filter",
            "requiredUIProfile": "platform.page.v2.15"
          }
        ],
        "outputPorts": [
          {
            "id": "selection",
            "bindingField": "selectionVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.14",
            "writable": true
          },
          {
            "id": "selected-records",
            "bindingField": "selectionSetVariable",
            "type": "record-set",
            "requiredUIProfile": "platform.page.v2.32",
            "writable": true
          }
        ],
        "configurationFields": [
          "actions",
          "facets",
          "fields",
          "filterSearchVariable",
          "inlineEdit",
          "model",
          "object",
          "parentSelection",
          "query",
          "relation",
          "selection",
          "showSearch",
          "tableColumns"
        ],
        "events": [
          {
            "id": "select",
            "payload": "void",
            "required": false,
            "maxBindings": 1,
            "requiredUIProfile": "platform.page.v2.33"
          }
        ],
        "slots": [
          {
            "id": "toolbar",
            "title": "Toolbar",
            "placement": "before",
            "requiredUIProfile": "platform.page.v2.104",
            "allowedLayouts": [
              "rows",
              "columns",
              "flow",
              "toolbar"
            ]
          },
          {
            "id": "footer",
            "title": "Footer",
            "placement": "after",
            "requiredUIProfile": "platform.page.v2.104",
            "allowedLayouts": [
              "rows",
              "columns",
              "flow",
              "toolbar"
            ]
          }
        ]
      },
      {
        "componentID": "detail",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "read",
        "bindingKinds": [
          "object",
          "fields",
          "selection",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.5"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "detailPresentation",
          "fields",
          "model",
          "object",
          "selection"
        ],
        "slots": [
          {
            "id": "actions",
            "title": "Actions",
            "placement": "after",
            "requiredUIProfile": "platform.page.v2.104",
            "allowedLayouts": [
              "rows",
              "columns",
              "flow",
              "toolbar"
            ]
          }
        ]
      },
      {
        "componentID": "actions",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "read",
        "bindingKinds": [
          "object",
          "actions",
          "selection",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.5"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "actions",
          "model",
          "object",
          "selection"
        ]
      },
      {
        "componentID": "chart",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "relation",
          "aggregate",
          "filter-variable",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.19",
            "scopeRule": "local"
          },
          {
            "id": "filter",
            "bindingField": "filterVariable",
            "type": "filter",
            "requiredUIProfile": "platform.page.v2.15"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object",
          "parentSelection",
          "relation"
        ]
      },
      {
        "componentID": "metric",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "relation",
          "aggregate",
          "filter-variable",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.19",
            "scopeRule": "local"
          },
          {
            "id": "filter",
            "bindingField": "filterVariable",
            "type": "filter",
            "requiredUIProfile": "platform.page.v2.15"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object",
          "parentSelection",
          "relation"
        ]
      },
      {
        "componentID": "text",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [],
        "outputPorts": []
      },
      {
        "componentID": "filter",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "fields",
          "filter-variable",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.30",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [
          {
            "id": "filter",
            "bindingField": "filterVariable",
            "type": "filter",
            "requiredUIProfile": "platform.page.v2.15",
            "writable": true
          }
        ],
        "configurationFields": [
          "facets",
          "fields",
          "filterSearchVariable",
          "model",
          "object"
        ]
      },
      {
        "componentID": "form",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "fields",
          "relation",
          "inputs"
        ],
        "inputPorts": [],
        "outputPorts": [],
        "configurationFields": [
          "fields",
          "inputs",
          "model",
          "object",
          "parentSelection",
          "relation"
        ]
      },
      {
        "componentID": "timeline",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "read",
        "bindingKinds": [
          "object",
          "selection",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.5"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object",
          "selection"
        ]
      },
      {
        "componentID": "tasks",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "read",
        "bindingKinds": [
          "object",
          "selection",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.5"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object",
          "selection"
        ]
      },
      {
        "componentID": "function",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "read",
        "bindingKinds": [
          "function",
          "selection"
        ],
        "inputPorts": [],
        "outputPorts": [],
        "configurationFields": [
          "function",
          "selection"
        ]
      },
      {
        "componentID": "compute",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.1",
        "selectionMode": "read",
        "bindingKinds": [
          "operation",
          "inputs",
          "selection"
        ],
        "inputPorts": [],
        "outputPorts": [],
        "configurationFields": [
          "inputs",
          "operation",
          "selection"
        ]
      },
      {
        "componentID": "button",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.4",
        "selectionMode": "none",
        "bindingKinds": [
          "interaction"
        ],
        "inputPorts": [
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.4"
          }
        ],
        "outputPorts": [],
        "events": [
          {
            "id": "click",
            "payload": "void",
            "required": true,
            "maxBindings": 1
          }
        ]
      },
      {
        "componentID": "input",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.7",
        "selectionMode": "none",
        "bindingKinds": [
          "state-variable"
        ],
        "inputPorts": [
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.7"
          }
        ],
        "outputPorts": []
      },
      {
        "componentID": "pivot",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.23",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "relation",
          "aggregate",
          "filter-variable",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.23",
            "scopeRule": "local"
          },
          {
            "id": "filter",
            "bindingField": "filterVariable",
            "type": "filter",
            "requiredUIProfile": "platform.page.v2.15"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object",
          "parentSelection",
          "relation"
        ]
      },
      {
        "componentID": "record-timeline",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.25",
        "selectionMode": "write",
        "bindingKinds": [
          "object",
          "selection",
          "collection-variable",
          "selection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.25",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [
          {
            "id": "selection",
            "bindingField": "selectionVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.25",
            "writable": true
          }
        ],
        "configurationFields": [
          "model",
          "object",
          "selection"
        ]
      },
      {
        "componentID": "kanban",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.26",
        "selectionMode": "write",
        "bindingKinds": [
          "object",
          "fields",
          "actions",
          "selection",
          "collection-variable",
          "selection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.26",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [
          {
            "id": "selection",
            "bindingField": "selectionVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.26",
            "writable": true
          }
        ],
        "configurationFields": [
          "actions",
          "fields",
          "group",
          "model",
          "object",
          "selection"
        ]
      },
      {
        "componentID": "record-view",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.36",
        "selectionMode": "read",
        "bindingKinds": [
          "object",
          "fields",
          "selection",
          "record-variable",
          "actions"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.5"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "actions",
          "fields",
          "model",
          "object",
          "recordView",
          "selection"
        ]
      },
      {
        "componentID": "button-group",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.37",
        "selectionMode": "none",
        "bindingKinds": [
          "interaction"
        ],
        "inputPorts": [
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.4"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "buttons"
        ],
        "events": [
          {
            "id": "click",
            "payload": "void",
            "required": true,
            "maxBindings": 16,
            "requiredUIProfile": "platform.page.v2.37"
          }
        ]
      },
      {
        "componentID": "record-links",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.38",
        "selectionMode": "read",
        "bindingKinds": [
          "object",
          "selection",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.5"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object",
          "recordLinks",
          "selection"
        ]
      },
      {
        "componentID": "status-tracker",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.39",
        "selectionMode": "read",
        "bindingKinds": [
          "object",
          "selection",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.5"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object",
          "selection",
          "statusTracker"
        ]
      },
      {
        "componentID": "heading",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.41",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [],
        "outputPorts": []
      },
      {
        "componentID": "collection-title",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.41",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable",
          "count-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.41",
            "required": true,
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          },
          {
            "id": "count",
            "bindingField": "countVariable",
            "type": "decimal",
            "requiredUIProfile": "platform.page.v2.41",
            "required": true,
            "modes": [
              "aggregate"
            ],
            "source": "count",
            "sameOwner": "collectionVariable",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object"
        ]
      },
      {
        "componentID": "record-list",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.42",
        "selectionMode": "write",
        "bindingKinds": [
          "object",
          "fields",
          "selection",
          "collection-variable",
          "selection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.10",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [
          {
            "id": "selection",
            "bindingField": "selectionVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.94",
            "writable": true
          }
        ],
        "configurationFields": [
          "actions",
          "fields",
          "model",
          "object",
          "selection"
        ],
        "fieldRoles": [
          {
            "path": "cardLabel",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          },
          {
            "path": "fields",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference",
              "integer",
              "decimal",
              "money",
              "date",
              "datetime",
              "boolean"
            ],
            "optional": true,
            "distinct": true
          }
        ]
      },
      {
        "componentID": "record-chart",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.43",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.43",
            "required": true,
            "plan": {
              "ordered": true
            },
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "recordChart.xField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference",
              "integer",
              "decimal",
              "boolean",
              "date",
              "datetime"
            ],
            "allowID": true
          },
          {
            "path": "recordChart.yField",
            "kinds": [
              "integer",
              "decimal"
            ]
          }
        ]
      },
      {
        "componentID": "record-events",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.45",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.45",
            "required": true,
            "plan": {
              "ordered": true
            },
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "recordEvents.timeField",
            "kinds": [
              "date",
              "datetime"
            ]
          },
          {
            "path": "recordEvents.titleField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          },
          {
            "path": "recordEvents.severityField",
            "kinds": [
              "choice"
            ]
          }
        ]
      },
      {
        "componentID": "record-calendar",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.46",
        "selectionMode": "write",
        "bindingKinds": [
          "object",
          "collection-variable",
          "selection"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.46",
            "required": true,
            "plan": {
              "ordered": true
            },
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object",
          "selection"
        ],
        "fieldRoles": [
          {
            "path": "recordCalendar.dateField",
            "kinds": [
              "date",
              "datetime"
            ]
          },
          {
            "path": "recordCalendar.labelField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          }
        ]
      },
      {
        "componentID": "record-gantt",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.47",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.47",
            "required": true,
            "plan": {
              "ordered": true
            },
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "recordGantt.startField",
            "kinds": [
              "date",
              "datetime"
            ]
          },
          {
            "path": "recordGantt.endField",
            "kinds": [
              "date",
              "datetime"
            ]
          },
          {
            "path": "recordGantt.titleField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          },
          {
            "path": "recordGantt.statusField",
            "kinds": [
              "choice"
            ]
          }
        ]
      },
      {
        "componentID": "progress",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.48",
        "selectionMode": "none",
        "bindingKinds": [
          "progress-value",
          "progress-total"
        ],
        "inputPorts": [
          {
            "id": "value",
            "bindingField": "progressValueVariable",
            "type": "decimal",
            "requiredUIProfile": "platform.page.v2.48",
            "required": true,
            "scopeRule": "accessible"
          },
          {
            "id": "total",
            "bindingField": "progressTotalVariable",
            "type": "decimal",
            "requiredUIProfile": "platform.page.v2.48",
            "scopeRule": "accessible"
          }
        ],
        "outputPorts": []
      },
      {
        "componentID": "gauge",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.49",
        "selectionMode": "none",
        "bindingKinds": [
          "gauge-value"
        ],
        "inputPorts": [
          {
            "id": "value",
            "bindingField": "gaugeValueVariable",
            "type": "number",
            "requiredUIProfile": "platform.page.v2.49",
            "required": true,
            "scopeRule": "accessible"
          }
        ],
        "outputPorts": []
      },
      {
        "componentID": "summary-stats",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.50",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable",
          "statistics-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.50",
            "required": true,
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          },
          {
            "id": "statistics",
            "bindingField": "statisticsVariable",
            "type": "statistics",
            "requiredUIProfile": "platform.page.v2.50",
            "required": true,
            "modes": [
              "aggregate"
            ],
            "source": "statistics",
            "sameOwner": "collectionVariable",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "summaryField",
            "kinds": [
              "integer",
              "decimal"
            ]
          }
        ]
      },
      {
        "componentID": "record-leaderboard",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.51",
        "selectionMode": "write",
        "bindingKinds": [
          "object",
          "collection-variable",
          "selection",
          "selection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.51",
            "required": true,
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [
          {
            "id": "selection",
            "bindingField": "selectionVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.95",
            "writable": true
          }
        ],
        "configurationFields": [
          "model",
          "object",
          "selection"
        ],
        "fieldRoles": [
          {
            "path": "leaderboard.valueField",
            "kinds": [
              "integer",
              "decimal"
            ]
          },
          {
            "path": "leaderboard.labelField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          }
        ]
      },
      {
        "componentID": "range-input",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.52",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [],
        "outputPorts": [
          {
            "id": "min",
            "bindingField": "rangeMinVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.52",
            "writable": true,
            "stateWritable": true,
            "required": true,
            "modes": [
              "state"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "scopeRule": "local"
          },
          {
            "id": "max",
            "bindingField": "rangeMaxVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.52",
            "writable": true,
            "stateWritable": true,
            "required": true,
            "modes": [
              "state"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "sameOwner": "rangeMinVariable",
            "scopeRule": "local"
          }
        ],
        "configurationFields": [
          "rangeInput"
        ],
        "portGroups": [
          {
            "fields": [
              "rangeMinVariable",
              "rangeMaxVariable"
            ],
            "min": 2,
            "max": 2,
            "distinct": true
          }
        ]
      },
      {
        "componentID": "boolean-input",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.53",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.53"
          }
        ],
        "outputPorts": [
          {
            "id": "value",
            "bindingField": "booleanVariable",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.53",
            "writable": true,
            "stateWritable": true,
            "required": true,
            "modes": [
              "state"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "scopeRule": "local"
          }
        ]
      },
      {
        "componentID": "choice-input",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.55",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.55"
          }
        ],
        "outputPorts": [
          {
            "id": "value",
            "bindingField": "choiceVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.55",
            "writable": true,
            "stateWritable": true,
            "scopeRule": "local"
          },
          {
            "id": "selection",
            "bindingField": "choiceSetVariable",
            "type": "string-set",
            "requiredUIProfile": "platform.page.v2.56",
            "writable": true,
            "stateWritable": true,
            "scopeRule": "local"
          }
        ],
        "configurationFields": [
          "choiceInput"
        ]
      },
      {
        "componentID": "date-input",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.57",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.57"
          }
        ],
        "outputPorts": [
          {
            "id": "value",
            "bindingField": "dateVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.57",
            "writable": true,
            "stateWritable": true,
            "required": true,
            "modes": [
              "state"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "scopeRule": "local"
          }
        ]
      },
      {
        "componentID": "record-picker",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.58",
        "selectionMode": "write",
        "bindingKinds": [
          "object",
          "collection-variable",
          "selection"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.58",
            "scopeRule": "local"
          },
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.58"
          }
        ],
        "outputPorts": [
          {
            "id": "value",
            "bindingField": "pickerValueVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.64",
            "writable": true,
            "stateWritable": true,
            "scopeRule": "accessible"
          }
        ],
        "configurationFields": [
          "model",
          "object",
          "recordPicker",
          "selection"
        ],
        "fieldRoles": [
          {
            "path": "recordPicker.labelField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          }
        ]
      },
      {
        "componentID": "alert-banner",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.60",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [
          {
            "id": "value",
            "bindingField": "alertValueVariable",
            "type": "decimal",
            "requiredUIProfile": "platform.page.v2.60",
            "required": true,
            "scopes": [
              "page",
              "overlay"
            ],
            "scopeRule": "local"
          }
        ],
        "outputPorts": []
      },
      {
        "componentID": "notice",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.61",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [],
        "outputPorts": []
      },
      {
        "componentID": "separator",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.62",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [],
        "outputPorts": []
      },
      {
        "componentID": "spacer",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.63",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [],
        "outputPorts": []
      },
      {
        "componentID": "term-counts",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.67",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "records",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.67",
            "required": true,
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "group",
            "kinds": [
              "text",
              "choice"
            ]
          }
        ]
      },
      {
        "componentID": "histogram",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.68",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "records",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.68",
            "required": true,
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "histogram",
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "histogram.field",
            "kinds": [
              "integer",
              "decimal"
            ]
          }
        ]
      },
      {
        "componentID": "record-scatter",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.69",
        "selectionMode": "write",
        "bindingKinds": [
          "object",
          "collection-variable",
          "selection",
          "selection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.69",
            "required": true,
            "plan": {
              "ordered": true,
              "maxLimit": 100
            },
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [
          {
            "id": "selection",
            "bindingField": "selectionVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.95",
            "writable": true
          }
        ],
        "configurationFields": [
          "model",
          "object",
          "selection"
        ],
        "fieldRoles": [
          {
            "path": "scatter.xField",
            "kinds": [
              "integer",
              "decimal"
            ]
          },
          {
            "path": "scatter.yField",
            "kinds": [
              "integer",
              "decimal"
            ]
          },
          {
            "path": "scatter.colorField",
            "kinds": [
              "text",
              "choice"
            ]
          },
          {
            "path": "scatter.labelField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          }
        ]
      },
      {
        "componentID": "heatmap",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.70",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable",
          "aggregate"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.70",
            "required": true,
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [
          {
            "id": "rowValue",
            "bindingField": "rowValueVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.70",
            "writable": true,
            "stateWritable": true,
            "modes": [
              "state"
            ],
            "sameOwner": "collectionVariable",
            "scopeRule": "local"
          },
          {
            "id": "rowSet",
            "bindingField": "rowSetVariable",
            "type": "string-set",
            "requiredUIProfile": "platform.page.v2.70",
            "writable": true,
            "stateWritable": true,
            "modes": [
              "state"
            ],
            "sameOwner": "collectionVariable",
            "scopeRule": "local"
          },
          {
            "id": "columnValue",
            "bindingField": "columnValueVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.70",
            "writable": true,
            "stateWritable": true,
            "modes": [
              "state"
            ],
            "sameOwner": "collectionVariable",
            "scopeRule": "local"
          },
          {
            "id": "columnSet",
            "bindingField": "columnSetVariable",
            "type": "string-set",
            "requiredUIProfile": "platform.page.v2.70",
            "writable": true,
            "stateWritable": true,
            "modes": [
              "state"
            ],
            "sameOwner": "collectionVariable",
            "scopeRule": "local"
          }
        ],
        "configurationFields": [
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "group",
            "kinds": [
              "text",
              "choice"
            ]
          },
          {
            "path": "columnGroup",
            "kinds": [
              "text",
              "choice"
            ]
          }
        ],
        "portGroups": [
          {
            "fields": [
              "rowValueVariable",
              "rowSetVariable"
            ],
            "min": 0,
            "max": 1
          },
          {
            "fields": [
              "columnValueVariable",
              "columnSetVariable"
            ],
            "min": 0,
            "max": 1
          },
          {
            "fields": [
              "rowValueVariable",
              "rowSetVariable",
              "columnValueVariable",
              "columnSetVariable"
            ],
            "min": 0,
            "max": 4,
            "distinct": true
          }
        ]
      },
      {
        "componentID": "treemap",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.71",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "records",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.71",
            "required": true,
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [
          {
            "id": "groupValue",
            "bindingField": "groupValueVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.71",
            "writable": true,
            "stateWritable": true,
            "modes": [
              "state"
            ],
            "sameOwner": "collectionVariable",
            "scopeRule": "local"
          },
          {
            "id": "groupSet",
            "bindingField": "groupSetVariable",
            "type": "string-set",
            "requiredUIProfile": "platform.page.v2.71",
            "writable": true,
            "stateWritable": true,
            "modes": [
              "state"
            ],
            "sameOwner": "collectionVariable",
            "scopeRule": "local"
          }
        ],
        "configurationFields": [
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "group",
            "kinds": [
              "text",
              "choice"
            ]
          }
        ],
        "portGroups": [
          {
            "fields": [
              "groupValueVariable",
              "groupSetVariable"
            ],
            "min": 0,
            "max": 1
          }
        ]
      },
      {
        "componentID": "sparkline-kpi",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.72",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.72",
            "required": false,
            "scopeRule": "local"
          },
          {
            "id": "sparklineDecimal",
            "bindingField": "sparklineDecimalVariable",
            "type": "decimal",
            "requiredUIProfile": "platform.page.v2.72",
            "required": false,
            "scopes": [
              "page",
              "overlay"
            ],
            "scopeRule": "local"
          },
          {
            "id": "sparklineNumber",
            "bindingField": "sparklineNumberVariable",
            "type": "number",
            "requiredUIProfile": "platform.page.v2.72",
            "required": false,
            "scopes": [
              "page",
              "overlay"
            ],
            "scopeRule": "local"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "sparkline.field",
            "kinds": [
              "integer",
              "decimal"
            ],
            "when": "collectionVariable"
          }
        ],
        "portGroups": [
          {
            "fields": [
              "sparklineDecimalVariable",
              "sparklineNumberVariable"
            ],
            "min": 1,
            "max": 1
          }
        ]
      },
      {
        "componentID": "record-card",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.73",
        "selectionMode": "read",
        "bindingKinds": [
          "object",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.73"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "fields",
          "model",
          "object"
        ],
        "slots": [
          {
            "id": "actions",
            "title": "Actions",
            "placement": "after",
            "requiredUIProfile": "platform.page.v2.104",
            "allowedLayouts": [
              "rows",
              "columns",
              "flow",
              "toolbar"
            ]
          }
        ],
        "fieldRoles": [
          {
            "path": "recordCard.labelField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          },
          {
            "path": "fields",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference",
              "integer",
              "decimal",
              "money",
              "date",
              "datetime",
              "boolean"
            ],
            "optional": true,
            "distinct": true
          }
        ]
      },
      {
        "componentID": "record-comparison",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.74",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "fields",
          "record-set-variable"
        ],
        "inputPorts": [
          {
            "id": "records",
            "bindingField": "recordSetVariable",
            "type": "record-set",
            "requiredUIProfile": "platform.page.v2.74"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "fields",
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "recordComparison.labelField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          },
          {
            "path": "fields",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference",
              "integer",
              "decimal",
              "money",
              "date",
              "datetime",
              "boolean"
            ],
            "optional": true,
            "distinct": true
          }
        ]
      },
      {
        "componentID": "tag-counts",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.75",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "records",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.75",
            "required": true,
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "exact"
          },
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.75"
          }
        ],
        "outputPorts": [
          {
            "id": "groupValue",
            "bindingField": "groupValueVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.75",
            "writable": true,
            "stateWritable": true,
            "modes": [
              "state"
            ],
            "sameOwner": "collectionVariable",
            "scopeRule": "local"
          }
        ],
        "configurationFields": [
          "model",
          "object"
        ],
        "fieldRoles": [
          {
            "path": "group",
            "kinds": [
              "text",
              "choice"
            ]
          }
        ],
        "portGroups": [
          {
            "fields": [
              "groupValueVariable"
            ],
            "min": 0,
            "max": 1
          }
        ]
      },
      {
        "componentID": "record-comments",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.76",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.76"
          },
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.76"
          }
        ],
        "outputPorts": [
          {
            "id": "draft",
            "bindingField": "commentDraftVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.76",
            "writable": true,
            "stateWritable": true
          }
        ],
        "configurationFields": [
          "model",
          "object"
        ],
        "resources": [
          {
            "id": "relations/object/platform.comment",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "relations",
                "kind": "object",
                "name": "platform.comment"
              }
            }
          },
          {
            "id": "relations/action/platform.comment.add",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "relations",
                "kind": "action",
                "name": "platform.comment.add"
              }
            }
          }
        ]
      },
      {
        "componentID": "record-uploader",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.76",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.76"
          },
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.76"
          }
        ],
        "outputPorts": [
          {
            "id": "file",
            "bindingField": "fileVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.76",
            "writable": true,
            "stateWritable": true
          }
        ],
        "configurationFields": [
          "model",
          "object"
        ],
        "resources": [
          {
            "id": "files/object/files.file",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "files",
                "kind": "object",
                "name": "files.file"
              }
            }
          },
          {
            "id": "files/action/files.file.attach",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "files",
                "kind": "action",
                "name": "files.file.attach"
              }
            }
          }
        ]
      },
      {
        "componentID": "media-preview",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.76",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.76"
          },
          {
            "id": "file",
            "bindingField": "fileVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.76"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object"
        ],
        "resources": [
          {
            "id": "files/object/files.file",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "files",
                "kind": "object",
                "name": "files.file"
              }
            }
          }
        ]
      },
      {
        "componentID": "pdf-viewer",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.76",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.76"
          },
          {
            "id": "file",
            "bindingField": "fileVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.76"
          }
        ],
        "outputPorts": [
          {
            "id": "page",
            "bindingField": "pdfPageVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.76",
            "writable": true,
            "stateWritable": true
          }
        ],
        "configurationFields": [
          "model",
          "object"
        ],
        "resources": [
          {
            "id": "files/object/files.file",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "files",
                "kind": "object",
                "name": "files.file"
              }
            }
          }
        ]
      },
      {
        "componentID": "approval-inbox",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.77",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.77"
          }
        ],
        "outputPorts": [],
        "resources": [
          {
            "id": "work/object/work.task",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "work",
                "kind": "object",
                "name": "work.task"
              }
            }
          },
          {
            "id": "work/object/work.approval",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "work",
                "kind": "object",
                "name": "work.approval"
              }
            }
          },
          {
            "id": "work/action/work.approval.approve",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "work",
                "kind": "action",
                "name": "work.approval.approve"
              }
            }
          },
          {
            "id": "work/action/work.approval.reject",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "work",
                "kind": "action",
                "name": "work.approval.reject"
              }
            }
          },
          {
            "id": "inbox",
            "kind": "owner-read",
            "source": "work",
            "url": "inbox"
          }
        ]
      },
      {
        "componentID": "notification-feed",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.77",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.77"
          }
        ],
        "outputPorts": [],
        "resources": [
          {
            "id": "platform/action/platform.notification.read",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "platform",
                "kind": "action",
                "name": "platform.notification.read"
              }
            }
          },
          {
            "id": "notifications",
            "kind": "owner-read",
            "source": "platform",
            "url": "notifications"
          }
        ]
      },
      {
        "componentID": "breadcrumb",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.78",
        "selectionMode": "none",
        "bindingKinds": [
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.78",
            "scopeRule": "exact"
          },
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.78"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "breadcrumb",
          "model",
          "object"
        ],
        "events": [
          {
            "id": "click",
            "payload": "void",
            "required": true,
            "maxBindings": 1,
            "requiredUIProfile": "platform.page.v2.78"
          }
        ]
      },
      {
        "componentID": "avatar-stack",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.78",
        "selectionMode": "none",
        "bindingKinds": [
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.78",
            "scopeRule": "exact"
          },
          {
            "id": "context",
            "bindingField": "avatar.contextVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.78",
            "scopeRule": "exact"
          },
          {
            "id": "context-collection",
            "bindingField": "avatar.contextCollectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.78",
            "scopeRule": "exact"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object"
        ]
      },
      {
        "componentID": "static-image",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.78",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [],
        "outputPorts": [],
        "configurationFields": [
          "image"
        ]
      },
      {
        "componentID": "resource-list",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.79",
        "selectionMode": "write",
        "bindingKinds": [
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.79",
            "scopeRule": "exact"
          },
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.79"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object",
          "resourceList"
        ],
        "fieldRoles": [
          {
            "path": "resourceList.labelField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          },
          {
            "path": "resourceList.statusField",
            "kinds": [
              "choice",
              "text"
            ]
          }
        ]
      },
      {
        "componentID": "asset-directory",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.79",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.79"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "assetDirectory"
        ],
        "events": [
          {
            "id": "click",
            "payload": "void",
            "required": false,
            "maxBindings": 4,
            "requiredUIProfile": "platform.page.v2.79"
          }
        ]
      },
      {
        "componentID": "graph-explorer",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.79",
        "selectionMode": "none",
        "bindingKinds": [
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.79",
            "scopeRule": "exact"
          },
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.79"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "graphExplorer",
          "model",
          "object"
        ]
      },
      {
        "componentID": "vertex-graph",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.79",
        "selectionMode": "none",
        "bindingKinds": [
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.79",
            "scopeRule": "exact"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object",
          "vertexGraph"
        ]
      },
      {
        "componentID": "collection-analysis",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.80",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.80",
            "required": true,
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "exact"
          },
          {
            "id": "x",
            "bindingField": "analysisXVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.80",
            "writable": true,
            "stateWritable": true,
            "scopeRule": "exact"
          },
          {
            "id": "y",
            "bindingField": "analysisYVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.80",
            "writable": true,
            "stateWritable": true,
            "scopeRule": "exact"
          },
          {
            "id": "count",
            "bindingField": "analysisCountVariable",
            "type": "decimal",
            "requiredUIProfile": "platform.page.v2.80",
            "writable": false,
            "scopeRule": "exact"
          },
          {
            "id": "mean",
            "bindingField": "analysisMeanVariable",
            "type": "number",
            "requiredUIProfile": "platform.page.v2.80",
            "writable": false,
            "scopeRule": "exact"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "analysis",
          "model",
          "object"
        ]
      },
      {
        "componentID": "action-table",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.81",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "actions",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.81",
            "writable": false,
            "required": true,
            "modes": [
              "resource"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "source": "plan",
            "scopeRule": "exact"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "actionTable",
          "actions",
          "model",
          "object"
        ]
      },
      {
        "componentID": "notepad",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.81",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [
          {
            "id": "text",
            "bindingField": "notepadVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.81",
            "writable": true,
            "stateWritable": true,
            "required": true,
            "modes": [
              "state"
            ],
            "scopes": [
              "page",
              "overlay"
            ],
            "scopeRule": "exact"
          }
        ],
        "outputPorts": []
      },
      {
        "componentID": "observation",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.82",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.82",
            "scopeRule": "exact"
          },
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.82"
          },
          {
            "id": "history",
            "bindingField": "observationHistoryVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.82",
            "scopeRule": "exact"
          },
          {
            "id": "context",
            "bindingField": "observationContextVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.82",
            "scopeRule": "exact"
          },
          {
            "id": "signal",
            "bindingField": "observationSignalVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.82",
            "writable": true,
            "stateWritable": true,
            "scopeRule": "exact"
          },
          {
            "id": "threshold",
            "bindingField": "observationThresholdVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.82",
            "writable": true,
            "stateWritable": true,
            "scopeRule": "exact"
          },
          {
            "id": "rows",
            "bindingField": "observationRowsVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.82",
            "writable": true,
            "stateWritable": true,
            "scopeRule": "exact"
          },
          {
            "id": "count",
            "bindingField": "observationCountVariable",
            "type": "decimal",
            "requiredUIProfile": "platform.page.v2.82",
            "scopeRule": "exact"
          },
          {
            "id": "mean",
            "bindingField": "observationMeanVariable",
            "type": "number",
            "requiredUIProfile": "platform.page.v2.82",
            "scopeRule": "exact"
          },
          {
            "id": "enabled",
            "bindingField": "enabledWhen",
            "type": "boolean",
            "requiredUIProfile": "platform.page.v2.82"
          }
        ],
        "outputPorts": [
          {
            "id": "asset",
            "bindingField": "observation.assetOutput",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.103",
            "writable": true,
            "scopeRule": "exact"
          }
        ],
        "configurationFields": [
          "model",
          "object",
          "observation"
        ]
      },
      {
        "componentID": "embedded-page",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.83",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [],
        "outputPorts": [],
        "configurationFields": [
          "embedding"
        ]
      },
      {
        "componentID": "external-frame",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.84",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [],
        "outputPorts": [],
        "configurationFields": [
          "externalFrame"
        ]
      },
      {
        "componentID": "ai-assistant",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.86",
        "selectionMode": "none",
        "bindingKinds": [],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.73",
            "scopeRule": "exact"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "ai",
          "function",
          "model",
          "object"
        ]
      },
      {
        "componentID": "record-map",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.87",
        "selectionMode": "write",
        "bindingKinds": [
          "object",
          "collection-variable",
          "selection",
          "selection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.87",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [
          {
            "id": "selection",
            "bindingField": "selectionVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.101",
            "writable": true
          }
        ],
        "configurationFields": [
          "model",
          "object",
          "selection"
        ],
        "fieldRoles": [
          {
            "path": "map.latitudeField",
            "kinds": [
              "integer",
              "decimal"
            ]
          },
          {
            "path": "map.longitudeField",
            "kinds": [
              "integer",
              "decimal"
            ]
          },
          {
            "path": "map.colorField",
            "kinds": [
              "text",
              "choice"
            ],
            "optional": true
          },
          {
            "path": "map.labelField",
            "kinds": [
              "text",
              "longtext",
              "choice",
              "reference"
            ],
            "allowID": true
          }
        ]
      },
      {
        "componentID": "image-annotation",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.87",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.87"
          },
          {
            "id": "file",
            "bindingField": "fileVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.87"
          }
        ],
        "outputPorts": [],
        "configurationFields": [
          "model",
          "object"
        ],
        "resources": [
          {
            "id": "files/object/files.file",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "files",
                "kind": "object",
                "name": "files.file"
              }
            }
          },
          {
            "id": "files/action/files.file.annotate",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "files",
                "kind": "action",
                "name": "files.file.annotate"
              }
            }
          }
        ]
      },
      {
        "componentID": "scene-3d",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.87",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "record-variable"
        ],
        "inputPorts": [
          {
            "id": "record",
            "bindingField": "recordVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.87",
            "scopeRule": "exact"
          },
          {
            "id": "file",
            "bindingField": "fileVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.87",
            "scopeRule": "exact"
          },
          {
            "id": "sample",
            "bindingField": "sceneSampleVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.87",
            "scopeRule": "exact"
          },
          {
            "id": "sample-window",
            "bindingField": "sceneSampleCollectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.87",
            "scopeRule": "exact"
          }
        ],
        "outputPorts": [
          {
            "id": "part",
            "bindingField": "scenePartVariable",
            "type": "string",
            "requiredUIProfile": "platform.page.v2.87",
            "writable": true,
            "stateWritable": true,
            "scopeRule": "exact"
          }
        ],
        "configurationFields": [
          "model",
          "object"
        ],
        "resources": [
          {
            "id": "files/object/files.file",
            "kind": "service",
            "asset": {
              "ref": {
                "app": "files",
                "kind": "object",
                "name": "files.file"
              }
            }
          }
        ]
      },
      {
        "componentID": "collection-builder",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.88",
        "selectionMode": "none",
        "bindingKinds": [
          "object",
          "collection-variable"
        ],
        "inputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.88",
            "scopeRule": "local"
          }
        ],
        "outputPorts": [
          {
            "id": "collection",
            "bindingField": "collectionOutputVariable",
            "type": "object-set",
            "requiredUIProfile": "platform.page.v2.88"
          }
        ],
        "configurationFields": [
          "model",
          "object"
        ]
      },
      {
        "componentID": "layered-graph",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.111",
        "selectionMode": "write",
        "bindingKinds": [
          "composite"
        ],
        "inputPorts": [],
        "outputPorts": [
          {
            "id": "selection",
            "bindingField": "selectionVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.111",
            "writable": true
          }
        ],
        "configurationFields": [
          "composite"
        ],
        "events": [
          {
            "id": "select",
            "payload": "void",
            "required": false,
            "maxBindings": 1
          }
        ],
        "modelPorts": [
          {
            "id": "nodes",
            "roles": {
              "label": {
                "kinds": [
                  "text",
                  "choice",
                  "reference",
                  "integer",
                  "number",
                  "decimal"
                ]
              },
              "detail": {
                "kinds": [
                  "text",
                  "choice",
                  "reference",
                  "integer",
                  "number",
                  "decimal"
                ]
              },
              "layer": {
                "kinds": [
                  "reference"
                ],
                "target": "layers"
              },
              "state": {
                "kinds": [
                  "text",
                  "choice"
                ]
              },
              "current": {
                "kinds": [
                  "boolean"
                ]
              },
              "notation": {
                "kinds": [
                  "text",
                  "choice"
                ]
              },
              "order": {
                "kinds": [
                  "integer",
                  "number",
                  "decimal"
                ]
              }
            },
            "required": true
          },
          {
            "id": "layers",
            "roles": {
              "label": {
                "kinds": [
                  "text",
                  "choice",
                  "reference",
                  "integer",
                  "number",
                  "decimal"
                ]
              }
            }
          },
          {
            "id": "edges",
            "roles": {
              "from": {
                "kinds": [
                  "reference"
                ],
                "required": true,
                "target": "nodes"
              },
              "to": {
                "kinds": [
                  "reference"
                ],
                "required": true,
                "target": "nodes"
              },
              "label": {
                "kinds": [
                  "text",
                  "choice",
                  "reference",
                  "integer",
                  "number",
                  "decimal"
                ]
              },
              "state": {
                "kinds": [
                  "text",
                  "choice"
                ]
              },
              "dashed": {
                "kinds": [
                  "boolean"
                ]
              }
            },
            "required": true
          },
          {
            "id": "overlay",
            "roles": {
              "node": {
                "kinds": [
                  "reference"
                ],
                "required": true,
                "target": "nodes"
              },
              "detail": {
                "kinds": [
                  "text",
                  "choice",
                  "reference",
                  "integer",
                  "number",
                  "decimal"
                ]
              },
              "state": {
                "kinds": [
                  "text",
                  "choice"
                ]
              },
              "current": {
                "kinds": [
                  "boolean"
                ]
              }
            }
          }
        ]
      },
      {
        "componentID": "model-matrix",
        "configVersion": 1,
        "requiredUIProfile": "platform.page.v2.111",
        "selectionMode": "write",
        "bindingKinds": [
          "composite"
        ],
        "inputPorts": [],
        "outputPorts": [
          {
            "id": "selection",
            "bindingField": "selectionVariable",
            "type": "record",
            "requiredUIProfile": "platform.page.v2.111",
            "writable": true
          }
        ],
        "configurationFields": [
          "composite"
        ],
        "events": [
          {
            "id": "select",
            "payload": "void",
            "required": false,
            "maxBindings": 1
          }
        ],
        "modelPorts": [
          {
            "id": "rows",
            "roles": {
              "label": {
                "kinds": [
                  "text",
                  "choice",
                  "reference",
                  "integer",
                  "number",
                  "decimal"
                ]
              }
            },
            "required": true
          },
          {
            "id": "columns",
            "roles": {
              "label": {
                "kinds": [
                  "text",
                  "choice",
                  "reference",
                  "integer",
                  "number",
                  "decimal"
                ]
              }
            },
            "required": true
          }
        ]
      }
    ],
    "supportedProfiles": [
      "platform.page.v2.1",
      "platform.page.v2.2",
      "platform.page.v2.3",
      "platform.page.v2.4",
      "platform.page.v2.5",
      "platform.page.v2.6",
      "platform.page.v2.7",
      "platform.page.v2.8",
      "platform.page.v2.9",
      "platform.page.v2.10",
      "platform.page.v2.11",
      "platform.page.v2.12",
      "platform.page.v2.13",
      "platform.page.v2.14",
      "platform.page.v2.15",
      "platform.page.v2.16",
      "platform.page.v2.17",
      "platform.page.v2.18",
      "platform.page.v2.19",
      "platform.page.v2.20",
      "platform.page.v2.21",
      "platform.page.v2.22",
      "platform.page.v2.23",
      "platform.page.v2.24",
      "platform.page.v2.25",
      "platform.page.v2.26",
      "platform.page.v2.27",
      "platform.page.v2.28",
      "platform.page.v2.29",
      "platform.page.v2.30",
      "platform.page.v2.31",
      "platform.page.v2.32",
      "platform.page.v2.33",
      "platform.page.v2.34",
      "platform.page.v2.35",
      "platform.page.v2.36",
      "platform.page.v2.37",
      "platform.page.v2.38",
      "platform.page.v2.39",
      "platform.page.v2.40",
      "platform.page.v2.41",
      "platform.page.v2.42",
      "platform.page.v2.43",
      "platform.page.v2.44",
      "platform.page.v2.45",
      "platform.page.v2.46",
      "platform.page.v2.47",
      "platform.page.v2.48",
      "platform.page.v2.49",
      "platform.page.v2.50",
      "platform.page.v2.51",
      "platform.page.v2.52",
      "platform.page.v2.53",
      "platform.page.v2.54",
      "platform.page.v2.55",
      "platform.page.v2.56",
      "platform.page.v2.57",
      "platform.page.v2.58",
      "platform.page.v2.59",
      "platform.page.v2.60",
      "platform.page.v2.61",
      "platform.page.v2.62",
      "platform.page.v2.63",
      "platform.page.v2.64",
      "platform.page.v2.65",
      "platform.page.v2.66",
      "platform.page.v2.67",
      "platform.page.v2.68",
      "platform.page.v2.69",
      "platform.page.v2.70",
      "platform.page.v2.71",
      "platform.page.v2.72",
      "platform.page.v2.73",
      "platform.page.v2.74",
      "platform.page.v2.75",
      "platform.page.v2.76",
      "platform.page.v2.77",
      "platform.page.v2.78",
      "platform.page.v2.79",
      "platform.page.v2.80",
      "platform.page.v2.81",
      "platform.page.v2.82",
      "platform.page.v2.83",
      "platform.page.v2.84",
      "platform.page.v2.85",
      "platform.page.v2.86",
      "platform.page.v2.87",
      "platform.page.v2.88",
      "platform.page.v2.89",
      "platform.page.v2.90",
      "platform.page.v2.91",
      "platform.page.v2.92",
      "platform.page.v2.93",
      "platform.page.v2.94",
      "platform.page.v2.95",
      "platform.page.v2.96",
      "platform.page.v2.97",
      "platform.page.v2.98",
      "platform.page.v2.99",
      "platform.page.v2.100",
      "platform.page.v2.101",
      "platform.page.v2.102",
      "platform.page.v2.103",
      "platform.page.v2.104",
      "platform.page.v2.105",
      "platform.page.v2.106",
      "platform.page.v2.107",
      "platform.page.v2.108",
      "platform.page.v2.109",
      "platform.page.v2.110",
      "platform.page.v2.111",
      "platform.page.v2.112"
    ],
    "documentless": {
      "widgets": [
        "inline-action",
        "table",
        "detail",
        "actions",
        "chart",
        "metric",
        "text",
        "filter",
        "form",
        "timeline",
        "tasks",
        "function",
        "compute",
        "button",
        "input",
        "pivot",
        "record-timeline",
        "kanban",
        "record-view",
        "button-group",
        "external-frame",
        "layered-graph",
        "model-matrix"
      ],
      "fields": [
        "actions",
        "buttons",
        "cardLabel",
        "collectionVariable",
        "columnGroup",
        "composite",
        "configVersion",
        "detailPresentation",
        "externalFrame",
        "facets",
        "fields",
        "filterSearchVariable",
        "filterVariable",
        "function",
        "group",
        "id",
        "inlineEdit",
        "inputs",
        "mark",
        "measure",
        "model",
        "object",
        "operation",
        "parentSelection",
        "query",
        "recordVariable",
        "recordView",
        "relation",
        "selection",
        "selectionSetVariable",
        "selectionVariable",
        "showSearch",
        "tableColumns",
        "text",
        "timeEnd",
        "timeGroup",
        "timeLabel",
        "timeStart",
        "title",
        "widget",
        "width"
      ]
    },
    "features": {
      "variables": "platform.page.v2.2",
      "resourceVariables": "platform.page.v2.3",
      "propertyVariables": "platform.page.v2.17",
      "decimalVariables": "platform.page.v2.16",
      "applicationFilters": "platform.page.v2.15",
      "applicationRecords": "platform.page.v2.14",
      "applicationCollections": "platform.page.v2.12",
      "applicationBindings": "platform.page.v2.8",
      "overlayFilters": "platform.page.v2.13",
      "overlayResources": "platform.page.v2.11",
      "overlayVariables": "platform.page.v2.7",
      "loopVariables": "platform.page.v2.5",
      "interactiveLayouts": "platform.page.v2.4",
      "statefulLayouts": "platform.page.v2.2",
      "layoutInput": "platform.page.v2.7",
      "chartMark": "platform.page.v2.24",
      "actionDefaults": "platform.page.v2.92"
    },
    "runtime": {
      "aggregate": {
        "source": "count",
        "maxVariables": 8,
        "maxExpandedReads": 32
      },
      "application": {
        "scope": "application",
        "valueTypes": [
          "string",
          "boolean",
          "object-set",
          "record",
          "filter",
          "decimal",
          "record-set"
        ],
        "modes": [
          "state",
          "constant",
          "derived",
          "resource",
          "property",
          "aggregate"
        ],
        "bindingMode": "shared",
        "maxFilterFields": 16
      },
      "composite": {
        "maxRecords": 200,
        "maxCells": 1600,
        "maxBytes": 262144,
        "requiredUIProfile": "platform.page.v2.111"
      },
      "computeResource": {
        "requiredUIProfile": "platform.page.v2.100",
        "maxResources": 4
      },
      "contextViews": {
        "requiredUIProfile": "platform.page.v2.78",
        "maxAvatarWindow": 6,
        "maxDetailFields": 2,
        "maxLabelBytes": 1024,
        "maxURLBytes": 4096,
        "maxCaptionBytes": 4096,
        "maxImageHeight": 4096,
        "defaultImageHeight": 160,
        "sharedUIProfile": "platform.page.v2.98"
      },
      "dateInput": {
        "requiredUIProfile": "platform.page.v2.57",
        "dateTimeRequiredUIProfile": "platform.page.v2.59"
      },
      "decimal": {
        "maxBytes": 128
      },
      "embedding": {
        "maxDepth": 3,
        "maxInstances": 16,
        "maxQueries": 8,
        "maxRecords": 512,
        "maxSections": 256
      },
      "exploration": {
        "requiredUIProfile": "platform.page.v2.79",
        "maxResourceWindow": 12,
        "maxDirectoryItems": 4,
        "maxObjects": 8,
        "maxRelations": 12,
        "maxPath": 8,
        "maxWindow": 20,
        "maxNeighborhoodRecords": 7,
        "sharedVertexUIProfile": "platform.page.v2.102"
      },
      "interface": {
        "maxPorts": 16,
        "maxVersion": 65535,
        "valueTypes": [
          "string",
          "boolean",
          "record",
          "decimal",
          "object-set"
        ]
      },
      "loop": {
        "scope": "loop-item",
        "source": "item",
        "maxContainers": 8,
        "maxItems": 100,
        "maxTotalItems": 256,
        "recordWidgets": [
          "detail",
          "actions",
          "timeline",
          "tasks",
          "inline-action",
          "record-view",
          "record-links",
          "status-tracker",
          "record-card",
          "record-comments",
          "record-uploader",
          "media-preview",
          "pdf-viewer"
        ],
        "presentationWidgets": [
          "text",
          "button",
          "input",
          "button-group",
          "progress",
          "gauge"
        ],
        "maxDepth": 2
      },
      "maxStringBytes": 4096,
      "maxVariables": 64,
      "modelBinding": {
        "requiredUIProfile": "platform.page.v2.110",
        "maxFields": 64,
        "maxPath": 16,
        "maxRecords": 10000,
        "maxWindow": 200,
        "maxBytes": 8388608,
        "fieldSlots": [
          "group",
          "columnGroup",
          "timeStart",
          "timeEnd",
          "timeLabel",
          "timeGroup",
          "cardLabel",
          "summaryField",
          "measure",
          "recordGantt.startField",
          "recordGantt.endField",
          "recordGantt.titleField",
          "recordGantt.statusField",
          "recordCalendar.dateField",
          "recordCalendar.labelField",
          "recordEvents.timeField",
          "recordEvents.titleField",
          "recordEvents.severityField",
          "recordPicker.labelField",
          "recordCard.labelField",
          "recordComparison.labelField",
          "recordChart.xField",
          "recordChart.yField",
          "scatter.xField",
          "scatter.yField",
          "scatter.labelField",
          "scatter.colorField",
          "histogram.field",
          "leaderboard.valueField",
          "leaderboard.labelField",
          "statusTracker.field",
          "breadcrumb.labelField",
          "avatar.labelField",
          "sparkline.field"
        ]
      },
      "operators": [
        {
          "id": "equal",
          "input": "same",
          "output": "boolean",
          "minArgs": 2,
          "maxArgs": 2
        },
        {
          "id": "not",
          "input": "boolean",
          "output": "boolean",
          "minArgs": 1,
          "maxArgs": 1
        },
        {
          "id": "and",
          "input": "boolean",
          "output": "boolean",
          "minArgs": 2,
          "maxArgs": 8
        },
        {
          "id": "or",
          "input": "boolean",
          "output": "boolean",
          "minArgs": 2,
          "maxArgs": 8
        },
        {
          "id": "concat",
          "input": "string",
          "output": "string",
          "minArgs": 2,
          "maxArgs": 8
        },
        {
          "id": "present",
          "input": "resource",
          "output": "boolean",
          "minArgs": 1,
          "maxArgs": 1
        },
        {
          "id": "decimal-add",
          "input": "decimal",
          "output": "decimal",
          "minArgs": 2,
          "maxArgs": 2
        },
        {
          "id": "decimal-subtract",
          "input": "decimal",
          "output": "decimal",
          "minArgs": 2,
          "maxArgs": 2
        },
        {
          "id": "decimal-less",
          "input": "decimal",
          "output": "boolean",
          "minArgs": 2,
          "maxArgs": 2
        },
        {
          "id": "parse-decimal",
          "input": "string",
          "output": "decimal",
          "minArgs": 1,
          "maxArgs": 1
        },
        {
          "id": "parse-number",
          "input": "string",
          "output": "number",
          "minArgs": 1,
          "maxArgs": 1
        }
      ],
      "overlay": {
        "scope": "overlay",
        "valueTypes": [
          "string",
          "boolean",
          "record",
          "object-set",
          "filter",
          "decimal",
          "string-set",
          "record-set",
          "number",
          "statistics"
        ],
        "modes": [
          "state",
          "constant",
          "derived",
          "resource",
          "property",
          "aggregate"
        ]
      },
      "query": {
        "source": "plan",
        "maxPlans": 8,
        "maxConditions": 16,
        "maxSort": 4,
        "maxLimit": 100,
        "maxTotalLimit": 512,
        "maxOffset": 100000,
        "operators": [
          "=",
          "!=",
          "\u003c",
          "\u003c=",
          "\u003e",
          "\u003e=",
          "like",
          "in",
          "not in"
        ],
        "set": {
          "operations": [
            "union",
            "intersect",
            "subtract"
          ],
          "maxNodes": 31,
          "maxDepth": 4,
          "maxBytes": 65536,
          "maxConditions": 32,
          "maxSearchBytes": 4096
        },
        "inventoryUIProfile": "platform.page.v2.97",
        "maxDeclaredPlans": 16,
        "maxDeclaredTotalLimit": 1600,
        "interfaceUIProfile": "platform.page.v2.109"
      },
      "recordSelection": {
        "maxRecords": 64,
        "sharedUIProfile": "platform.page.v2.96"
      },
      "resources": [
        {
          "kind": "record",
          "type": "record",
          "widget": "table",
          "widgets": [
            "table",
            "record-list"
          ]
        },
        {
          "kind": "filter",
          "type": "filter",
          "widget": "filter"
        },
        {
          "kind": "query",
          "type": "object-set",
          "widget": "table",
          "widgets": [
            "table",
            "record-list"
          ]
        },
        {
          "kind": "record",
          "widget": "record-timeline",
          "type": "record",
          "widgets": [
            "record-timeline",
            "record-list"
          ]
        },
        {
          "kind": "record",
          "widget": "kanban",
          "type": "record",
          "widgets": [
            "kanban",
            "record-list"
          ]
        },
        {
          "kind": "records",
          "type": "record-set",
          "widget": "table"
        },
        {
          "kind": "record",
          "type": "record",
          "widget": "record-calendar"
        },
        {
          "kind": "record",
          "type": "record",
          "widget": "record-leaderboard"
        },
        {
          "kind": "record",
          "type": "record",
          "widget": "record-picker"
        },
        {
          "kind": "record",
          "type": "record",
          "widget": "record-scatter"
        },
        {
          "kind": "record",
          "type": "record",
          "widget": "resource-list"
        },
        {
          "kind": "record",
          "type": "record",
          "widget": "graph-explorer"
        },
        {
          "kind": "record",
          "type": "record",
          "widget": "observation"
        },
        {
          "kind": "record",
          "type": "record",
          "widget": "record-map"
        },
        {
          "kind": "query",
          "type": "object-set",
          "widget": "collection-builder"
        }
      ],
      "scope": "page",
      "tableEditing": {
        "maxRows": 64,
        "maxFields": 16,
        "fieldTypes": [
          "text",
          "longtext",
          "integer",
          "decimal",
          "boolean",
          "choice",
          "date",
          "datetime",
          "reference"
        ]
      },
      "telemetry": {
        "sharedUIProfile": "platform.page.v2.103"
      },
      "valueTypes": [
        "string",
        "boolean",
        "record",
        "filter",
        "object-set",
        "decimal",
        "string-set",
        "record-set",
        "number",
        "statistics"
      ]
    }
  },
  "arrayFieldPaths": [
    "avatar.detailFields",
    "collectionBuilder.fields",
    "fields"
  ],
  "repeatedFieldPaths": [
    "tableColumns.field",
    "scene.mappings.field",
    "observation.signals.field",
    "recordLinks.field"
  ]
} as const;
