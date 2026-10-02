import inventory from "./widgets.json" with {type:"json"};

/** Migration discovery only; renderer eligibility and grants stay in their owners. */
export const workshopMigrationCatalog=inventory;
if(inventory.entries.length!==inventory.source.widgetTypeCount||new Set(inventory.entries.map(e=>e.sourceType)).size!==92)throw new Error("Invalid Workshop migration inventory");
export const workshopMapping=(type:string)=>inventory.entries.find(e=>e.sourceType===type);
