export type WorkflowStep = { name: string; title?: string; ask?: string; answers?: string[]; act?: string; function?: { name: string; version: number }; next?: string; branches?: Record<string, string> };
export type WorkflowDraft = { id: string; revision: number; name: string; title: string; object: string; when: string; steps: WorkflowStep[]; version?: number; published?: string };
export type WorkflowObject = { id: string; name: string; title: string; published?: string; states?: { name: string; title: string }[];
  actions?: { name: string; title: string; inputs?: { required?: boolean }[]; approval?: unknown }[]; access?: { role: string; read: string }[] };

/** Source pickers use the installed definition while its draft changes. */
export function installedObjects<T extends WorkflowObject>(records: T[]): T[] {
  return records.flatMap((record) => {
    if (!record.published) return [];
    try { return [JSON.parse(record.published) as T]; }
    catch { return []; }
  });
}
