export const renderingLimits = {
  "runtime": {
    "telemetry": {
      "sharedUIProfile": "platform.page.v2.103"
    },
    "scope": "page",
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
    ],
    "maxVariables": 64,
    "maxStringBytes": 4096,
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
        "<",
        "<=",
        ">",
        ">=",
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
    "decimal": {
      "maxBytes": 128
    },
    "aggregate": {
      "source": "count",
      "maxVariables": 8,
      "maxExpandedReads": 32
    },
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
    "recordSelection": {
      "maxRecords": 64,
      "sharedUIProfile": "platform.page.v2.96"
    },
    "tablePresentation": {
      "requiredUIProfile": "platform.page.v2.34",
      "maxColumns": 64,
      "maxTitleBytes": 256,
      "minWidth": 40,
      "maxWidth": 1200,
      "formatters": [
        {
          "id": "text",
          "fieldTypes": [
            "text",
            "longtext",
            "integer",
            "decimal",
            "boolean",
            "date",
            "datetime",
            "choice",
            "reference"
          ]
        },
        {
          "id": "numeric",
          "fieldTypes": [
            "integer",
            "decimal"
          ]
        },
        {
          "id": "date",
          "fieldTypes": [
            "date",
            "datetime"
          ]
        },
        {
          "id": "badge",
          "fieldTypes": [
            "text",
            "choice"
          ]
        }
      ]
    },
    "detailPresentation": {
      "requiredUIProfile": "platform.page.v2.35",
      "maxColumns": 4,
      "barcodeRequiredUIProfile": "platform.page.v2.107"
    },
    "recordView": {
      "requiredUIProfile": "platform.page.v2.36",
      "tabs": [
        "overview",
        "properties",
        "links",
        "history"
      ]
    },
    "buttonGroup": {
      "requiredUIProfile": "platform.page.v2.37",
      "maxButtons": 16,
      "maxTitleBytes": 256,
      "variants": [
        "default",
        "primary",
        "ghost",
        "danger"
      ],
      "icons": [
        "arrow",
        "edit",
        "plus",
        "trash"
      ]
    },
    "recordLinks": {
      "requiredUIProfile": "platform.page.v2.38",
      "maxGroups": 16,
      "maxTitleBytes": 256
    },
    "statusTracker": {
      "requiredUIProfile": "platform.page.v2.39",
      "maxStages": 32
    },
    "metricPresentation": {
      "requiredUIProfile": "platform.page.v2.40",
      "maxUnitBytes": 64,
      "formatters": [
        "number",
        "short"
      ],
      "variants": [
        "card",
        "simple",
        "tag"
      ],
      "tones": [
        "neutral",
        "warning",
        "danger",
        "success"
      ]
    },
    "titles": {
      "requiredUIProfile": "platform.page.v2.41",
      "maxTextBytes": 4096,
      "headingLevels": [
        "h1",
        "h2",
        "h3"
      ]
    },
    "recordList": {
      "requiredUIProfile": "platform.page.v2.42",
      "maxFields": 4,
      "layouts": [
        "grid",
        "list",
        "tiles"
      ]
    },
    "recordChart": {
      "requiredUIProfile": "platform.page.v2.43",
      "marks": [
        "bar",
        "line"
      ],
      "maxPoints": 40
    },
    "chartPresentation": {
      "requiredUIProfile": "platform.page.v2.44",
      "variants": [
        "pie",
        "donut"
      ]
    },
    "recordEvents": {
      "requiredUIProfile": "platform.page.v2.45",
      "maxEvents": 30,
      "maxTones": 32,
      "tones": [
        "neutral",
        "info",
        "success",
        "warning",
        "danger"
      ]
    },
    "recordCalendar": {
      "requiredUIProfile": "platform.page.v2.46",
      "maxRecords": 100
    },
    "recordGantt": {
      "requiredUIProfile": "platform.page.v2.47",
      "maxTones": 32,
      "tones": [
        "neutral",
        "info",
        "success",
        "warning",
        "danger"
      ],
      "maxRows": 20
    },
    "progress": {
      "requiredUIProfile": "platform.page.v2.48"
    },
    "gauge": {
      "requiredUIProfile": "platform.page.v2.49"
    },
    "summary": {
      "requiredUIProfile": "platform.page.v2.50"
    },
    "leaderboard": {
      "requiredUIProfile": "platform.page.v2.51",
      "maxRanks": 32
    },
    "rangeInput": {
      "requiredUIProfile": "platform.page.v2.52",
      "maxTicks": 10000
    },
    "booleanInput": {
      "requiredUIProfile": "platform.page.v2.54",
      "variants": [
        "switch",
        "checkbox"
      ]
    },
    "choiceInput": {
      "requiredUIProfile": "platform.page.v2.55",
      "variants": [
        "select",
        "radio",
        "segments",
        "multiple",
        "steps",
        "tabs"
      ],
      "maxOptions": 64,
      "maxOptionBytes": 256,
      "multipleRequiredUIProfile": "platform.page.v2.56",
      "maxSelected": 64,
      "clearRequiredUIProfile": "platform.page.v2.65",
      "indexedRequiredUIProfile": "platform.page.v2.75"
    },
    "dateInput": {
      "requiredUIProfile": "platform.page.v2.57",
      "dateTimeRequiredUIProfile": "platform.page.v2.59"
    },
    "recordPicker": {
      "requiredUIProfile": "platform.page.v2.58",
      "maxCandidates": 20,
      "valueRequiredUIProfile": "platform.page.v2.64"
    },
    "alertBanner": {
      "requiredUIProfile": "platform.page.v2.60",
      "maxMessageBytes": 4096,
      "tones": [
        "info",
        "warning",
        "danger"
      ]
    },
    "notice": {
      "requiredUIProfile": "platform.page.v2.61",
      "maxTitleBytes": 1024,
      "maxMessageBytes": 4096,
      "tones": [
        "info",
        "success",
        "warning",
        "danger"
      ]
    },
    "separator": {
      "requiredUIProfile": "platform.page.v2.62",
      "maxLabelBytes": 1024
    },
    "spacer": {
      "requiredUIProfile": "platform.page.v2.63"
    },
    "input": {
      "searchRequiredUIProfile": "platform.page.v2.66",
      "scanRequiredUIProfile": "platform.page.v2.107"
    },
    "terms": {
      "requiredUIProfile": "platform.page.v2.67",
      "maxGroups": 64
    },
    "histogram": {
      "requiredUIProfile": "platform.page.v2.68",
      "maxBins": 64
    },
    "recordScatter": {
      "requiredUIProfile": "platform.page.v2.69",
      "maxPoints": 100
    },
    "heatmap": {
      "requiredUIProfile": "platform.page.v2.70",
      "maxAxis": 64,
      "maxCells": 4096
    },
    "treemap": {
      "requiredUIProfile": "platform.page.v2.71"
    },
    "sparkline": {
      "requiredUIProfile": "platform.page.v2.72",
      "maxPoints": 30
    },
    "recordCard": {
      "requiredUIProfile": "platform.page.v2.73",
      "maxFields": 4,
      "tones": [
        "neutral",
        "info",
        "success",
        "warning",
        "danger"
      ]
    },
    "recordComparison": {
      "requiredUIProfile": "platform.page.v2.74",
      "minRecords": 2,
      "maxRecords": 4,
      "maxFields": 64,
      "minFields": 1
    },
    "collaboration": {
      "requiredUIProfile": "platform.page.v2.76",
      "maxCommentsWindow": 50,
      "maxFilesWindow": 100,
      "maxPreviewBytes": 26214400,
      "maxCommentBytes": 16384,
      "maxImagePixels": 16777216,
      "maxPDFPixels": 16777216,
      "maxPDFPages": 1000
    },
    "workViews": {
      "requiredUIProfile": "platform.page.v2.77",
      "maxApprovalsWindow": 25,
      "maxNotificationsWindow": 40,
      "maxHistoryWindow": 100,
      "defaultHistoryWindow": 18
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
    "collectionAnalysis": {
      "requiredUIProfile": "platform.page.v2.80",
      "maxGroups": 64,
      "maxRecords": 80
    },
    "recordWork": {
      "requiredUIProfile": "platform.page.v2.81",
      "maxActionRows": 50,
      "maxTiles": 8,
      "maxParameters": 16
    },
    "observation": {
      "requiredUIProfile": "platform.page.v2.82",
      "maxSignals": 100,
      "maxRecords": 100,
      "maxWindowRows": 100000
    },
    "embedding": {
      "maxDepth": 3,
      "maxInstances": 16,
      "maxQueries": 8,
      "maxRecords": 512,
      "maxSections": 256
    },
    "kanban": {
      "dynamicUIProfile": "platform.page.v2.99"
    },
    "computeResource": {
      "requiredUIProfile": "platform.page.v2.100",
      "maxResources": 4
    },
    "recordMap": {
      "sharedUIProfile": "platform.page.v2.101"
    },
    "device": {
      "requiredUIProfile": "platform.page.v2.107",
      "kinds": [
        "handheld"
      ]
    },
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
    "composite": {
      "maxRecords": 200,
      "maxCells": 1600,
      "maxBytes": 262144,
      "requiredUIProfile": "platform.page.v2.111"
    }
  },
  "layout": {
    "requiredUIProfile": "platform.page.v2.27",
    "minSize": 32,
    "maxSize": 4096,
    "maxWeight": 24,
    "maxGap": 64,
    "stackBelow": 448,
    "unusedProfile": "platform.page.v2.28",
    "maxUnused": 128,
    "presentationProfile": "platform.page.v2.93",
    "maxPadding": 64
  }
} as const;
