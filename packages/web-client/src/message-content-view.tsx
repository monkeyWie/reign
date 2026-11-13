import { useMemo } from "react";
import { Check } from "lucide-react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { parseMessageContent } from "./message-content";

export function MessageContent({ role, text }: { role: string; text: string }) {
  const parts = useMemo(
    () => (role === "user" ? parseMessageContent(text) : [{ text }]),
    [role, text],
  );
  return (
    <div className="message-parts">
      {parts.map((part, i) =>
        part.replies ? (
          <section
            className="question-replies"
            key={i}
            aria-label="已回答的问题"
          >
            <div className="question-replies-heading">
              <Check size={14} />
              <span>已回答</span>
            </div>
            {part.replies.map((reply, j) => (
              <div className="question-reply" key={j}>
                <div className="question-reply-question">{reply.question}</div>
                <div className="question-reply-answer">
                  <span>你的回答</span>
                  <p>{reply.answer || "未填写"}</p>
                </div>
              </div>
            ))}
          </section>
        ) : part.text.trim() ? (
          <div className="markdown" key={i}>
            <Markdown
              remarkPlugins={[remarkGfm]}
              components={{
                a: (props) => (
                  <a {...props} target="_blank" rel="noopener noreferrer" />
                ),
                img: () => null,
              }}
            >
              {part.text}
            </Markdown>
          </div>
        ) : null,
      )}
    </div>
  );
}
