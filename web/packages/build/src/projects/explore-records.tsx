import type { Api } from "@platform/kernel";
import { useEffect, useState } from "react";
import { SetBuilder } from "@platform/app";

export function ExploreRecords({ object }: { object: string }) {
  const [set, setSet] = useState<Api.ObjectSet>({ object });
  useEffect(() => { setSet({ object }); }, [object]);
  return <div className="grid min-w-0 gap-2 px-1 text-xs">
    <SetBuilder draftKey={`explore:${object}`} value={set} onChange={setSet} interfaces={undefined} />
  </div>;
}
