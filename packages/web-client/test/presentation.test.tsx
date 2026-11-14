import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  messages,
  showThinking,
  activityLabel,
  navigationRows,
  uniqueSessions,
} from "../src/presentation";
import { applyEvents, type Transcript } from "../src/transcript";
import type { Session } from "@reignhub/protocol";
const fixtures = JSON.parse(
  readFileSync(
    new URL("../../protocol/testdata/chat-presentation.json", import.meta.url),
    "utf8",
  ),
);
for (const fixture of fixtures)
  test(`native parity: ${fixture.name}`, () => {
    const rows = messages(fixture.transcript);
    assert.deepEqual(
      rows.map((r) => r.id),
      fixture.ids,
    );
    assert.deepEqual(
      rows.map((r) => r.role),
      fixture.roles,
    );
    assert.deepEqual(
      rows.map((r) => r.live),
      fixture.live,
    );
    assert.equal(showThinking(fixture.transcript), fixture.thinking);
    if (rows.some((r) => r.tools))
      assert.equal(
        activityLabel(rows.find((r) => r.tools)!.tools!),
        "正在搜索引用",
      );
  });
test("navigator samples all history, including first and latest message", () => {
  const rows = Array.from({ length: 120 }, (_, i) => ({
    id: String(i),
    role: "user",
    type: "userMessage",
    text: "Hi",
    turn: "t",
    live: false,
  }));
  const nav = navigationRows(rows);
  assert.equal(nav.length, 24);
  assert.equal(nav[0].id, "0");
  assert.equal(nav.at(-1)?.id, "119");
  assert.equal(new Set(nav.map((r) => r.id)).size, 24);
});
test("codex shared catalogue deduplicates to the originating provider and hides VS Code", () => {
  const session = {
    id: "same",
    cwd: "/project",
    title: "Test",
    preview: "",
    status: "idle",
    updatedAt: 1,
  } as Session;
  assert.deepEqual(
    uniqueSessions([
      { ...session, providerId: "codex-cli", sourceKind: "app", updatedAt: 10 },
      { ...session, providerId: "codex-app", sourceKind: "app" },
      {
        ...session,
        id: "vscode",
        providerId: "codex-cli",
        sourceKind: "vscode",
      },
      { ...session, providerId: "claude-code", updatedAt: 20 },
    ]).map((s) => s.providerId),
    ["claude-code", "codex-app"],
  );
});
test("stream creates messages from early deltas, handles UTF-16 and overlapping replay without losing suffix", () => {
  const t: Transcript = {
    session: { id: "s", status: "running" } as Session,
    turns: [],
  };
  const first = applyEvents(t, [
    {
      id: 1,
      sessionId: "s",
      turnId: "t",
      itemId: "a",
      kind: "text",
      offset: 0,
      delta: "你😀",
    },
  ]);
  const next = applyEvents(first, [
    {
      id: 2,
      sessionId: "s",
      turnId: "t",
      itemId: "a",
      kind: "text",
      offset: 1,
      delta: "😀好",
    },
    {
      id: 3,
      sessionId: "s",
      turnId: "t",
      itemId: "a",
      kind: "item",
      item: {
        id: "a",
        role: "assistant",
        type: "agentMessage",
        phase: "final_answer",
        text: "你",
      },
    },
    {
      id: 4,
      sessionId: "s",
      turnId: "t",
      itemId: "a",
      kind: "text",
      offset: 0,
      delta: "你😀",
    },
  ]);
  assert.equal(next.turns[0].items[0].text, "你😀好");
  assert.equal(first.turns[0].items[0].text, "你😀");
  assert.equal(t.turns.length, 0);
  assert.throws(
    () =>
      applyEvents(next, [
        {
          id: 5,
          sessionId: "s",
          turnId: "t",
          itemId: "a",
          kind: "text",
          offset: 20,
          delta: "gap",
        },
      ]),
    /STREAM_GAP/,
  );
  const done = applyEvents(next, [
    { id: 6, sessionId: "s", turnId: "t", kind: "turn", status: "completed" },
    { id: 7, sessionId: "s", turnId: "t", kind: "turn", status: "inProgress" },
    {
      id: 8,
      sessionId: "s",
      turnId: "t",
      itemId: "a",
      kind: "text",
      offset: 4,
      delta: "stale",
    },
  ]);
  assert.equal(done.turns[0].status, "completed");
  assert.equal(done.turns[0].items[0].text, "你😀好");
});

test("lagging full snapshots retain streamed suffix and loaded history, final snapshots retire activities", async () => {
  const { reconcileTranscript } = await import("../src/transcript");
  const current = structuredClone(fixtures[1].transcript) as Transcript;
  const incoming = structuredClone(current);
  incoming.turns.shift();
  incoming.turns[0].items = incoming.turns[0].items.slice(0, 3);
  incoming.turns[0].items[2].text = "Checking";
  const merged = reconcileTranscript(incoming, current);
  assert.deepEqual(
    merged.turns.map((t) => t.id),
    ["old", "live"],
  );
  assert.equal(
    merged.turns[1].items.find((i) => i.id === "a")?.text,
    "Checking files",
  );
  assert.equal(merged.turns[1].items.length, current.turns[1].items.length);
  const final = structuredClone(incoming);
  final.turns[0].status = "completed";
  final.turns[0].items.push({
    id: "final",
    role: "assistant",
    type: "agentMessage",
    text: "Done",
  });
  const completed = reconcileTranscript(final, merged);
  assert.deepEqual(
    messages(completed).map((r) => r.id),
    ["old|u", "live|u", "live|final"],
  );
  assert.equal(showThinking(completed), false);
});

test("tool details are collapsed and contain activity summaries rather than raw commands/output", async () => {
  const { createElement } = await import("react");
  const { renderToStaticMarkup } = await import("react-dom/server");
  const { Activity } = await import("../src/remote-app");
  const tools = [
    {
      id: "tool",
      role: "tool",
      type: "commandExecution",
      status: "inProgress",
      text: "RAW_TOOL_SECRET",
      activities: [{ kind: "read", title: "读取文件", detail: "src/app.go" }],
    },
  ];
  const html = renderToStaticMarkup(createElement(Activity, { tools }));
  assert.ok(html.includes("正在读取文件"));
  assert.ok(html.includes("src/app.go"));
  assert.ok(!html.includes("RAW_TOOL_SECRET"));
  assert.ok(!html.includes(" open="));
  assert.ok(!html.includes("message-actions"));
  const offline = renderToStaticMarkup(
    createElement(Activity, { tools, running: false }),
  );
  assert.ok(!offline.includes('class="shimmer"'));
});
