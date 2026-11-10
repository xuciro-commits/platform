export const registeredManifests = {
  "agent": {
    "id": "agent",
    "title": "Agent Studio"
  },
  "ai": {
    "id": "ai",
    "title": "AI"
  },
  "build": {
    "id": "build",
    "title": "Files"
  },
  "enterprise": {
    "id": "enterprise",
    "title": "Organisation"
  },
  "files": {
    "id": "files",
    "title": "Files"
  },
  "flow": {
    "id": "flow",
    "title": "Automate"
  },
  "knowledge": {
    "id": "knowledge",
    "title": "Knowledge"
  },
  "monitoring": {
    "id": "monitoring",
    "title": "Monitoring"
  },
  "platform": {
    "id": "platform",
    "title": "Control Panel"
  },
  "relations": {
    "id": "relations",
    "title": "Relations"
  },
  "system": {
    "id": "system",
    "title": "Platform"
  },
  "work": {
    "id": "work",
    "title": "Work"
  }
} as const;
export const registeredApplications = {
  "agents": {
    "id": "agents",
    "title": "Agent Studio",
    "manifest": "agent",
    "serves": [
      "build",
      "agent",
      "knowledge"
    ],
    "category": "build",
    "portal": true,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder"
        ]
      },
      {
        "app": "agent"
      },
      {
        "app": "knowledge"
      }
    ],
    "home": [
      {
        "route": {
          "view": "agents"
        },
        "when": [
          {
            "app": "agent"
          }
        ]
      },
      {
        "route": {
          "view": "agent"
        },
        "when": [
          {
            "app": "build",
            "roles": [
              "builder"
            ]
          }
        ]
      },
      {
        "route": {
          "view": "knowledge"
        }
      }
    ],
    "opens": [
      {
        "type": "build.agent",
        "view": "agent",
        "when": [
          {
            "app": "build",
            "roles": [
              "builder"
            ]
          }
        ]
      }
    ]
  },
  "automate": {
    "id": "automate",
    "title": "Automate",
    "manifest": "flow",
    "serves": [
      "build",
      "flow"
    ],
    "category": "build",
    "portal": true,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "automation"
        }
      }
    ],
    "opens": [
      {
        "type": "build.process/automation",
        "view": "automation"
      },
      {
        "type": "build.alertrule",
        "view": "alert"
      }
    ]
  },
  "build": {
    "id": "build",
    "title": "Files",
    "manifest": "build",
    "serves": [
      "build"
    ],
    "category": "build",
    "portal": false,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder",
          "viewer",
          "discoverer"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "files"
        },
        "shell": true
      }
    ],
    "opens": [
      {
        "type": "build.app",
        "view": "project"
      }
    ]
  },
  "catalog": {
    "id": "catalog",
    "title": "Asset Library",
    "serves": [
      "catalog"
    ],
    "surface": "developer",
    "category": "developer",
    "portal": false,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder"
        ]
      },
      {
        "app": "platform",
        "roles": [
          "admin"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "catalog"
        }
      }
    ],
    "opens": []
  },
  "data-connection": {
    "id": "data-connection",
    "title": "Data Connection",
    "serves": [
      "build",
      "platform"
    ],
    "category": "build",
    "portal": true,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder"
        ]
      },
      {
        "app": "build",
        "roles": [
          "integrator"
        ]
      },
      {
        "app": "platform",
        "roles": [
          "admin"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "connection"
        },
        "when": [
          {
            "app": "build",
            "roles": [
              "builder"
            ]
          },
          {
            "app": "build",
            "roles": [
              "integrator"
            ]
          }
        ]
      },
      {
        "route": {
          "view": "endpoints"
        }
      }
    ],
    "opens": [
      {
        "type": "build.connection",
        "view": "connection",
        "when": [
          {
            "app": "build",
            "roles": [
              "builder"
            ]
          },
          {
            "app": "build",
            "roles": [
              "integrator"
            ]
          }
        ]
      },
      {
        "type": "build.source",
        "view": "data-source",
        "when": [
          {
            "app": "build",
            "roles": [
              "builder"
            ]
          },
          {
            "app": "build",
            "roles": [
              "integrator"
            ]
          }
        ]
      },
      {
        "type": "build.observation",
        "view": "observation-source",
        "when": [
          {
            "app": "build",
            "roles": [
              "builder"
            ]
          },
          {
            "app": "build",
            "roles": [
              "integrator"
            ]
          }
        ]
      },
      {
        "type": "build.listener",
        "view": "listener",
        "when": [
          {
            "app": "build",
            "roles": [
              "builder"
            ]
          },
          {
            "app": "build",
            "roles": [
              "integrator"
            ]
          }
        ]
      },
      {
        "type": "build.writeback",
        "view": "writeback",
        "when": [
          {
            "app": "build",
            "roles": [
              "builder"
            ]
          },
          {
            "app": "build",
            "roles": [
              "integrator"
            ]
          }
        ]
      }
    ]
  },
  "data-lineage": {
    "id": "data-lineage",
    "title": "Data Lineage",
    "serves": [],
    "category": "govern",
    "portal": true,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder",
          "integrator"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "lineage"
        }
      }
    ],
    "opens": []
  },
  "dataset-preview": {
    "id": "dataset-preview",
    "title": "Dataset Preview",
    "serves": [
      "build"
    ],
    "category": "build",
    "portal": true,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "dataset"
        }
      }
    ],
    "opens": [
      {
        "type": "build.dataset",
        "view": "dataset"
      },
      {
        "type": "build.mediaset",
        "view": "media-set"
      }
    ]
  },
  "devops": {
    "id": "devops",
    "title": "DevOps",
    "serves": [
      "build"
    ],
    "category": "govern",
    "portal": true,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder"
        ]
      },
      {
        "app": "build",
        "roles": [
          "publisher"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "release-review"
        }
      }
    ],
    "opens": [
      {
        "type": "build.testplan",
        "view": "test-plan"
      },
      {
        "type": "build.testplan/evaluation-set",
        "view": "evaluation-set"
      },
      {
        "type": "build.sample-set",
        "view": "sample-set"
      }
    ]
  },
  "host-console": {
    "id": "host-console",
    "title": "Host Console",
    "serves": [],
    "category": "govern",
    "portal": true,
    "access": [
      {
        "all": true
      }
    ],
    "home": [
      {
        "route": {
          "view": "host-overview"
        }
      }
    ],
    "opens": []
  },
  "logic": {
    "id": "logic",
    "title": "Logic",
    "serves": [
      "build"
    ],
    "category": "build",
    "portal": true,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "functions"
        }
      }
    ],
    "opens": [
      {
        "type": "build.table",
        "view": "decision-table"
      },
      {
        "type": "build.process",
        "view": "flow"
      },
      {
        "type": "build.function",
        "view": "function"
      },
      {
        "type": "build.code",
        "view": "code"
      }
    ]
  },
  "marketplace": {
    "id": "marketplace",
    "title": "Marketplace",
    "serves": [
      "platform"
    ],
    "category": "govern",
    "portal": true,
    "access": [
      {
        "app": "platform",
        "roles": [
          "admin",
          "auditor"
        ]
      },
      {
        "schema": "build.app.create"
      }
    ],
    "home": [
      {
        "route": {
          "view": "apps"
        }
      }
    ],
    "opens": []
  },
  "monitoring": {
    "id": "monitoring",
    "title": "Monitoring",
    "manifest": "monitoring",
    "serves": [
      "platform",
      "build"
    ],
    "category": "govern",
    "portal": true,
    "access": [
      {
        "app": "platform",
        "roles": [
          "admin"
        ]
      },
      {
        "app": "flow"
      },
      {
        "app": "build",
        "roles": [
          "builder",
          "integrator"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "monitoring-overview"
        },
        "when": [
          {
            "app": "platform",
            "roles": [
              "admin"
            ]
          },
          {
            "app": "flow"
          },
          {
            "app": "build",
            "roles": [
              "builder"
            ]
          }
        ]
      },
      {
        "route": {
          "view": "integration-health"
        }
      }
    ],
    "opens": []
  },
  "my-work": {
    "id": "my-work",
    "title": "Notifications",
    "serves": [],
    "category": "govern",
    "portal": false,
    "access": [
      {
        "all": true
      }
    ],
    "home": [
      {
        "route": {
          "view": "notifications"
        }
      }
    ],
    "opens": []
  },
  "object-explorer": {
    "id": "object-explorer",
    "title": "Object Explorer",
    "serves": [
      "build"
    ],
    "category": "analyze",
    "portal": true,
    "access": [
      {
        "entities": true
      }
    ],
    "home": [
      {
        "route": {
          "view": "explorer"
        }
      }
    ],
    "opens": [
      {
        "type": "build.query",
        "view": "object-set",
        "when": [
          {
            "app": "build",
            "roles": [
              "builder"
            ]
          }
        ]
      }
    ]
  },
  "ontology": {
    "id": "ontology",
    "title": "Ontology Manager",
    "serves": [
      "build"
    ],
    "surface": "studio",
    "category": "build",
    "portal": true,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "object-type"
        }
      }
    ],
    "opens": [
      {
        "type": "build.actiontype",
        "view": "action-type"
      },
      {
        "type": "build.object",
        "view": "object-type"
      },
      {
        "type": "build.linktype",
        "view": "link-type"
      },
      {
        "type": "build.interface",
        "view": "interfaces"
      },
      {
        "type": "build.propertytype",
        "view": "property-type"
      },
      {
        "type": "build.valuetype",
        "view": "value-type"
      },
      {
        "type": "build.objecttypegroup",
        "view": "object-type-group"
      }
    ]
  },
  "pipeline-builder": {
    "id": "pipeline-builder",
    "title": "Pipeline Builder",
    "serves": [
      "build"
    ],
    "category": "build",
    "portal": true,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "pipeline"
        }
      }
    ],
    "opens": [
      {
        "type": "build.pipeline",
        "view": "pipeline"
      },
      {
        "type": "build.schedule",
        "view": "schedule"
      },
      {
        "type": "build.match",
        "view": "match"
      }
    ]
  },
  "platform": {
    "id": "platform",
    "title": "Control Panel",
    "manifest": "platform",
    "serves": [
      "platform",
      "ai"
    ],
    "surface": "tenant",
    "category": "govern",
    "portal": true,
    "access": [
      {
        "all": true
      }
    ],
    "home": [
      {
        "route": {
          "view": "control-panel"
        }
      }
    ],
    "opens": [
      {
        "type": "platform.marking",
        "view": "marking"
      },
      {
        "type": "platform.member",
        "view": "member",
        "when": [
          {
            "app": "platform",
            "roles": [
              "admin",
              "auditor"
            ]
          }
        ]
      }
    ]
  },
  "workshop": {
    "id": "workshop",
    "title": "Workshop",
    "serves": [
      "build"
    ],
    "category": "build",
    "portal": true,
    "access": [
      {
        "app": "build",
        "roles": [
          "builder"
        ]
      }
    ],
    "home": [
      {
        "route": {
          "view": "module"
        }
      }
    ],
    "opens": [
      {
        "type": "build.page",
        "view": "module"
      }
    ]
  }
} as const;
export type RegisteredManifestID = keyof typeof registeredManifests;
export type RegisteredApplicationID = keyof typeof registeredApplications;
