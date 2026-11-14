export const CONNECTION_STAGE_TIMEOUT = 120_000;

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

export function archiveProgress(steps: ConnectionProgress[], error?: string) {
  const round = crypto.randomUUID();
  const history: ConnectionProgress[] = steps.slice(-60).map((step) => ({
    ...step,
    id: step.id.startsWith("history:")
      ? step.id
      : `history:${round}:${step.id}`,
    state: step.state === "running" ? ("warning" as const) : step.state,
  }));
  if (error)
    history.push(
      progressStep(
        `history:${round}:failure`,
        "retry",
        "上一轮连接失败",
        error,
        "warning",
      ),
    );
  return [
    ...history,
    progressStep("retry", "retry", "开始新一轮连接", "", "info"),
  ];
}
