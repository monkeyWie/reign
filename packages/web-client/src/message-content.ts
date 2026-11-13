export interface QuestionReply {
  question: string;
  answer: string;
}
export type MessagePart =
  | { text: string; replies?: never }
  | { text?: never; replies: QuestionReply[] };

const open = "<send_user_message_question_reply>";
const close = "</send_user_message_question_reply>";

// Display projection only: the stored/source message remains unchanged.
export function parseMessageContent(text: string): MessagePart[] {
  if (!text.includes(open)) return text ? [{ text }] : [];
  const lines: { start: number; end: number; value: string }[] = [];
  for (let at = 0; at < text.length;) {
    const newline = text.indexOf("\n", at);
    const end = newline < 0 ? text.length : newline + 1;
    const raw = text.slice(at, end).replace(/[\r\n]+$/, "");
    const leading = raw.match(/^ */)![0].length;
    const value =
      leading <= 3 ? raw.slice(leading).replace(/[ \t]+$/, "") : raw;
    lines.push({ start: at, end, value });
    at = end;
  }
  const parts: MessagePart[] = [];
  let cursor = 0,
    fenceChar = "",
    fenceLength = 0;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const fence = line.value.match(/^(`+|~+)(.*)$/);
    if (fenceLength) {
      if (
        fence &&
        fence[1][0] === fenceChar &&
        fence[1].length >= fenceLength &&
        !fence[2].trim()
      )
        fenceLength = 0;
      continue;
    }
    if (fence && fence[1].length >= 3) {
      fenceChar = fence[1][0];
      fenceLength = fence[1].length;
      continue;
    }
    if (line.value !== open) continue;
    let end = i + 1;
    while (end < lines.length && lines[end].value !== close) end++;
    if (end === lines.length) break;
    let replies: QuestionReply[] | undefined;
    try {
      const input: unknown = JSON.parse(text.slice(line.end, lines[end].start));
      if (
        Array.isArray(input) &&
        input.length &&
        input.every(
          (q) =>
            q &&
            typeof q === "object" &&
            typeof q.question === "string" &&
            q.question.trim() &&
            (typeof q.answer === "string" ||
              (Array.isArray(q.answer) &&
                q.answer.every((a: unknown) => typeof a === "string"))),
        )
      )
        replies = input.map((q) => ({
          question: q.question,
          answer: typeof q.answer === "string" ? q.answer : q.answer.join("\n"),
        }));
    } catch {
      /* Preserve malformed payloads as source text. */
    }
    if (replies) {
      if (cursor < line.start)
        parts.push({ text: text.slice(cursor, line.start) });
      parts.push({ replies });
      cursor = lines[end].end;
    }
    i = end;
  }
  if (cursor < text.length) parts.push({ text: text.slice(cursor) });
  return parts;
}

export function messagePlainText(parts: MessagePart[]): string {
  const structured = parts.some((p) => p.replies);
  return parts
    .flatMap((p) =>
      p.replies
        ? p.replies.map(
            (q) => `问题：${q.question}\n回答：${q.answer || "未填写"}`,
          )
        : [structured ? p.text.trim() : p.text],
    )
    .filter(Boolean)
    .join(structured ? "\n\n" : "");
}

export function readableMessage(item: { role: string; text: string }): string {
  return item.role === "user"
    ? messagePlainText(parseMessageContent(item.text))
    : item.text;
}
