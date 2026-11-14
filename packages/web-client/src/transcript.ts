import type { Item, Session } from "@reignhub/protocol";
export interface Turn {
  id: string;
  status: string;
  items: Item[];
}
export interface Transcript {
  session: Session;
  turns: Turn[];
  nextCursor?: string;
}
export interface StreamEvent {
  id: number;
  sessionId: string;
  turnId?: string;
  itemId?: string;
  kind: string;
  status?: string;
  item?: Item;
  delta?: string;
  offset?: number;
  summary?: string;
}
export interface Batch {
  epoch: string;
  cursor: number;
  reset?: boolean;
  events: StreamEvent[];
}
export function applyEvents(
  input: Transcript,
  events: StreamEvent[],
): Transcript {
  const t = structuredClone(input);
  for (const e of events) {
    let turn = t.turns.find((x) => x.id === e.turnId);
    if (!turn && e.turnId) {
      turn = { id: e.turnId, status: "inProgress", items: [] };
      t.turns.push(turn);
    }
    if (
      e.kind === "turn" &&
      ["inProgress", "running", "in_progress"].includes(e.status || "")
    ) {
      if (turn) turn.status = e.status || "inProgress";
      t.session.status = "running";
      t.session.activeTurnId = e.turnId;
    }
    if (
      e.kind === "turn" &&
      !["inProgress", "running", "in_progress"].includes(e.status || "")
    ) {
      if (turn) turn.status = e.status || "completed";
      t.session.status = "idle";
      t.session.activeTurnId = undefined;
    }
    if (!turn) continue;
    if (e.item) {
      const at = turn.items.findIndex((x) => x.id === e.item!.id);
      if (at < 0) turn.items.push(e.item);
      else turn.items[at] = e.item;
    }
    if (e.delta !== undefined) {
      const item = turn.items.find((x) => x.id === e.itemId);
      if (item) {
        const offset = e.offset ?? item.text.length;
        if (offset >= 0 && offset <= item.text.length)
          item.text = item.text.slice(0, offset) + e.delta;
      }
    }
  }
  return t;
}
