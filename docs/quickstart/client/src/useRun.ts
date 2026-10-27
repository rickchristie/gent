import { useEffect, useState } from "react";
import { connectRun } from "./connection";
import type { State } from "./state";

export function useRun(runId: string) {
  const [state, setState] = useState<State | null>(null);
  const [status, setStatus] = useState("Choose a run");
  useEffect(() => {
    setState(null);
    if (!runId) return;
    let frame = 0;
    let latest: State;
    const stop = connectRun(
      runId,
      (next) => {
        latest = next;
        // Apply every event, but render at most once per browser frame.
        if (!frame)
          frame = requestAnimationFrame(() => {
            frame = 0;
            setState(latest);
          });
      },
      setStatus,
    );
    return () => {
      stop();
      cancelAnimationFrame(frame);
    };
  }, [runId]);
  return { state, status };
}
