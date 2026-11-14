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
const running = (s?: string) =>
  ["inProgress", "running", "in_progress"].includes(s || "");
export function applyEvents(
  input: Transcript,
  events: StreamEvent[],
): Transcript {
  const t = structuredClone(input);
  for (const e of events) {
    if (!e.turnId) continue;
    let turn = t.turns.find((x) => x.id === e.turnId);
    if (!turn) {
      turn = { id: e.turnId, status: "inProgress", items: [] };
      t.turns.push(turn);
    }
    const terminal = ["completed", "failed", "interrupted"].includes(
      turn.status,
    );
    if (e.kind === "turn") {
      if (!terminal || !running(e.status))
        turn.status = e.status || "completed";
      if (turn === t.turns.at(-1)) {
        t.session.status = running(turn.status) ? "running" : "idle";
        t.session.activeTurnId = running(turn.status) ? turn.id : undefined;
      }
      continue;
    }
    const id = e.item?.id || e.itemId;
    if (!id) continue;
    const at = turn.items.findIndex((x) => x.id === id);
    const prior = at < 0 ? undefined : turn.items[at];
    let item: Item = prior
      ? { ...prior }
      : { id, role: "assistant", type: "agentMessage", text: "" };
    if (e.item) {
      item = { ...item, ...e.item };
      if (prior?.text.startsWith(item.text)) item.text = prior.text;
      if (terminal || prior?.status === "completed")
        item.status = prior?.status || "completed";
    } else if (e.kind === "summary") {
      if (terminal) continue;
      item.role = "reasoning";
    } else if (e.delta !== undefined) {
      if (terminal) continue;
      // Codex offsets and JS strings both use UTF-16 code units. Snapshots may
      // already contain part/all of a replayed delta: append only its new suffix.
      const offset = e.offset ?? item.text.length;
      if (offset < 0 || offset > item.text.length) throw Error("STREAM_GAP");
      const overlap = Math.min(item.text.length - offset, e.delta.length);
      if (
        item.text.slice(offset, offset + overlap) !== e.delta.slice(0, overlap)
      )
        throw Error("STREAM_GAP");
      item.text += e.delta.slice(overlap);
    } else continue;
    if (at < 0) turn.items.push(item);
    else turn.items[at] = item;
  }
  return t;
}

// A full snapshot can lag the live stream. Keep newer active text and already
// loaded history, while accepting terminal snapshots as authoritative.
export function reconcileTranscript(
  next: Transcript,
  previous?: Transcript,
): Transcript {
  if (
    !previous ||
    previous.session.id !== next.session.id ||
    previous.session.providerId !== next.session.providerId
  )
    return next;
  const old = new Map(previous.turns.map((t) => [t.id, t]));
  const result = structuredClone(next);
  for (const turn of result.turns) {
    const prior = old.get(turn.id);
    if (!prior || !running(turn.status)) continue;
    if (!running(prior.status)) {
      Object.assign(turn, prior);
      continue;
    }
    const incoming = new Map(turn.items.map((i) => [i.id, i]));
    turn.items = prior.items.map((i) => {
      const fresh = incoming.get(i.id);
      if (!fresh) return i;
      incoming.delete(i.id);
      return {
        ...fresh,
        text: i.text.startsWith(fresh.text) ? i.text : fresh.text,
        status: i.status === "completed" ? i.status : fresh.status,
      };
    });
    turn.items.push(...incoming.values());
  }
  const anchor = previous.turns.findIndex((t) => t.id === result.turns[0]?.id);
  if (anchor > 0) {
    const ids = new Set(result.turns.map((t) => t.id));
    result.turns.unshift(
      ...previous.turns.slice(0, anchor).filter((t) => !ids.has(t.id)),
    );
    result.nextCursor = previous.nextCursor;
  }
  return result;
}
