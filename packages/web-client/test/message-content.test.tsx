import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import {
  parseMessageContent,
  messagePlainText,
  readableMessage,
  type MessagePart,
} from "../src/message-content";
import { MessageContent } from "../src/message-content-view";

const fixtures = JSON.parse(
  readFileSync(
    new URL(
      "../../../pkg/messagecontent/testdata/replies.json",
      import.meta.url,
    ),
    "utf8",
  ),
) as {
  name: string;
  source: string;
  parts: MessagePart[];
  plain: string;
}[];
for (const f of fixtures) {
  test(f.name, () => {
    const parts = parseMessageContent(f.source);
    assert.deepEqual(parts, f.parts);
    assert.equal(messagePlainText(parts), f.plain);
    assert.equal(readableMessage({ role: "user", text: f.source }), f.plain);
    assert.equal(
      readableMessage({ role: "assistant", text: f.source }),
      f.source,
    );
  });
}
test("renders replies as safe text without internal transport fields", () => {
  const source = fixtures.find((f) => f.name === "markup remains text")!.source;
  const html = renderToStaticMarkup(
    createElement(MessageContent, { role: "user", text: source }),
  );
  assert.match(html, /已回答/);
  assert.match(html, /你的回答/);
  assert.match(html, /&lt;script&gt;/);
  assert.doesNotMatch(
    html,
    /<script>|questionItemId|<send_user_message_question_reply>/,
  );
  const actual = renderToStaticMarkup(
    createElement(MessageContent, { role: "user", text: fixtures[0].source }),
  );
  assert.doesNotMatch(
    actual,
    /internal-call|questionItemId|send_user_message_question_reply/,
  );
});
