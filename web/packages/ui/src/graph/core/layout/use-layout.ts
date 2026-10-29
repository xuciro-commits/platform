import { useEffect, useRef, useState } from "react";
export function useCanvasLayout<T>(compute: () => Promise<T>, signature: string) {
  const current = useRef(compute); current.current = compute;
  const [state, setState] = useState<{ data?: T; error?: string; pending: boolean }>({ pending: true });
  useEffect(() => {
    let cancelled = false;
    setState((old) => ({ ...old, error: undefined, pending: true }));
    void current.current().then((data) => { if (!cancelled) setState({ data, pending: false }); },
      (error) => { if (!cancelled) setState((old) => ({ ...old, pending: false, error: error instanceof Error ? error.message : String(error) })); });
    return () => { cancelled = true; };
  }, [signature]);
  return state;
}
