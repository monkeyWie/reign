export interface ConnectionProgress {
  id: string;
  stage: string;
  message: string;
  detail?: string;
  state: "running" | "success" | "info" | "warning";
  at: number;
}
export type ProgressObserver = (step: ConnectionProgress) => void;
export function progressStep(
  id: string,
  stage: string,
  message: string,
  detail = "",
  state: ConnectionProgress["state"] = "running",
): ConnectionProgress {
  return { id, stage, message, detail, state, at: Date.now() };
}
export function recordProgress(
  steps: ConnectionProgress[],
  next: ConnectionProgress,
) {
  const at = steps.findIndex((step) => step.id === next.id);
  if (at < 0) return [...steps, next].slice(-80);
  if (
    steps[at].message === next.message &&
    steps[at].detail === next.detail &&
    steps[at].state === next.state
  )
    return steps;
  return steps.map((step, i) => (i === at ? { ...next, at: step.at } : step));
}
