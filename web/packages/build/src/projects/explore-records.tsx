import type { Api } from "@platform/kernel";
import { useEffect, useState } from "react";
import { ObjectSetEditor } from "../editor/ObjectSetEditor";

export function ExploreRecords({ object }: { object: string }) {
  const [set, setSet] = useState<Api.ObjectSet>({ object });
  useEffect(() => { setSet({ object }); }, [object]);
  return <div className="grid min-w-0 gap-2 px-1 text-xs">
    <ObjectSetEditor draftKey={`explore:${object}`} value={set} onChange={setSet} interfaces={undefined} />
  </div>;
}
