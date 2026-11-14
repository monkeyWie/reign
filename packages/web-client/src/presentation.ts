import type { Item, Session } from "@reignhub/protocol";
import type { Transcript } from "./transcript";

export const isRunning = (s?: string) =>
  ["running", "inProgress", "in_progress"].includes(s || "");
export interface MessageRow extends Item {
  turn: string;
  live: boolean;
  tools?: Item[];
}
const hiddenTools = new Set([
  "functionCallOutput",
  "hookPrompt",
  "contextCompaction",
  "enteredReviewMode",
  "exitedReviewMode",
]);
export const visibleTool = (item: Item) =>
  item.role === "tool" && !hiddenTools.has(item.type);
export function activeTurn(t?: Transcript) {
  const turn = t?.turns.at(-1);
  return turn && isRunning(turn.status) ? turn : undefined;
}
// Match the native client's presentation: tool results never become chat bubbles.
export function messages(t?: Transcript): MessageRow[] {
  const active = activeTurn(t)?.id;
  const out: MessageRow[] = [];
  for (const turn of t?.turns || []) {
    const live = turn.id === active;
    let tools: Item[] = [];
    const flush = () => {
      if (!tools.length) return;
      out.push({
        id: `${turn.id}|tools|${tools[0].id}`,
        role: "activity",
        type: "activity",
        text: "",
        turn: turn.id,
        live,
        tools,
      });
      tools = [];
    };
    for (const item of turn.items) {
      if (live && visibleTool(item)) {
        tools.push(item);
        continue;
      }
      if (
        !["user", "assistant"].includes(item.role) ||
        ["reasoning", "thinking"].includes(item.type) ||
        (!live && item.phase === "commentary") ||
        (!item.text.trim() && !item.attachments?.length)
      )
        continue;
      flush();
      out.push({
        ...item,
        id: `${turn.id}|${item.id}`,
        turn: turn.id,
        live: live && item.role === "assistant",
        text: displayText(item),
      });
    }
    flush();
  }
  return out;
}
export function showThinking(t?: Transcript) {
  const turn = activeTurn(t);
  return (
    !!turn &&
    !turn.items.some(
      (item) =>
        visibleTool(item) || (item.role === "assistant" && !!item.text.trim()),
    )
  );
}
export function activityLabel(tools: Item[]) {
  const labels: Record<string, string> = {
    read: "读取文件",
    list: "列出目录",
    search: "搜索",
    command: "运行命令",
    edit: "编辑文件",
    web: "浏览网页",
    tool: "调用工具",
    agent: "子 Agent",
    image: "图片",
    plan: "更新计划",
  };
  const counts = new Map<string, number>();
  let running = "";
  for (const tool of tools) {
    for (const a of tool.activities?.length
      ? tool.activities
      : [{ kind: "tool", title: `执行工具 ${tool.type}` }]) {
      counts.set(a.kind, (counts.get(a.kind) || 0) + 1);
      if (isRunning(tool.status) && tool.activities?.length)
        running = "正在" + a.title;
    }
  }
  return (
    running ||
    [...counts].map(([k, n]) => `${labels[k] || "活动"} ${n}`).join(" · ")
  );
}
export function navigationRows(rows: MessageRow[], limit = 24) {
  const users = rows.filter((r) => r.role === "user");
  if (users.length <= limit) return users;
  return Array.from(
    { length: limit },
    (_, i) => users[Math.round((i * (users.length - 1)) / (limit - 1))],
  );
}
export function uniqueSessions(sessions: Session[]) {
  const unique = new Map<string, Session>();
  const preferred = (s: Session) => s.providerId === `codex-${s.sourceKind}`;
  for (const s of sessions) {
    if (s.providerId === "codex-vscode" || s.sourceKind === "vscode") continue;
    const family = s.providerId.startsWith("codex-") ? "codex" : s.providerId;
    const key = `${family}|${s.id}`,
      old = unique.get(key);
    if (
      !old ||
      (preferred(s) && !preferred(old)) ||
      (preferred(s) === preferred(old) && s.updatedAt > old.updatedAt)
    )
      unique.set(key, s);
  }
  return [...unique.values()].sort(
    (a, b) =>
      b.updatedAt - a.updatedAt ||
      `${a.providerId}${a.id}`.localeCompare(`${b.providerId}${b.id}`),
  );
}
export function displayText(item: Item) {
  let body = item.text;
  if (item.role !== "user" || !item.attachments?.length) return body;
  const sources = new Set(item.attachments.map((a) => a.source));
  const marker = "## My request:",
    at = body.indexOf(marker);
  if (
    at >= 0 &&
    body.trimStart().startsWith("# Files mentioned by the user:")
  ) {
    const entries = [...body.slice(0, at).matchAll(/^## [^\n]+?: (.+)$/gm)];
    if (entries.length && entries.every((e) => sources.has(e[1].trim())))
      body = body.slice(at + marker.length).trim();
  }
  for (const prefix of ["ReignHub", "Agent Hub", "RAS"]) {
    const at = body.indexOf(
      `${prefix} 上传的附件（已保存在本机，可按需读取）：`,
    );
    if (at < 0) continue;
    const entries = [...body.slice(at).matchAll(/\]\(<([^>]+)>\)/g)];
    if (entries.length && entries.every((e) => sources.has(e[1])))
      body = body.slice(0, at).trim();
  }
  return body;
}
