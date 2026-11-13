// Package messagecontent projects transport wrappers into display-only content.
// It never changes the original message or treats replies as new instructions.
package messagecontent

import (
	"encoding/json"
	"strings"
)

type Reply struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type Part struct {
	Text    string  `json:"text,omitempty"`
	Replies []Reply `json:"replies,omitempty"`
}

const open = "<send_user_message_question_reply>"
const close = "</send_user_message_question_reply>"

type line struct {
	start, end int
	value      string
}

func fence(s string) (byte, int, string) {
	if len(s) == 0 || s[0] != '`' && s[0] != '~' {
		return 0, 0, ""
	}
	n := 1
	for n < len(s) && s[n] == s[0] {
		n++
	}
	return s[0], n, s[n:]
}

// Parse recognizes complete, standalone reply blocks outside Markdown code
// fences. Malformed/unknown payloads remain text, including their wrappers.
func Parse(text string) []Part {
	if !strings.Contains(text, open) {
		if text == "" {
			return nil
		}
		return []Part{{Text: text}}
	}
	lines := []line{}
	for at := 0; at < len(text); {
		end := strings.IndexByte(text[at:], '\n')
		if end < 0 {
			end = len(text)
		} else {
			end += at + 1
		}
		value := strings.TrimRight(text[at:end], "\r\n")
		trimmed := strings.TrimLeft(value, " ")
		if len(value)-len(trimmed) <= 3 {
			value = strings.TrimRight(trimmed, " \t")
		}
		lines = append(lines, line{at, end, value})
		at = end
	}
	parts := []Part{}
	cursor, fenceLength := 0, 0
	var fenceChar byte
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		char, length, rest := fence(l.value)
		if fenceLength > 0 {
			if char == fenceChar && length >= fenceLength && strings.TrimSpace(rest) == "" {
				fenceLength = 0
			}
			continue
		}
		if length >= 3 {
			fenceChar, fenceLength = char, length
			continue
		}
		if l.value != open {
			continue
		}
		end := i + 1
		for end < len(lines) && lines[end].value != close {
			end++
		}
		if end == len(lines) {
			break
		}
		var input []struct {
			Question string          `json:"question"`
			Answer   json.RawMessage `json:"answer"`
		}
		err := json.Unmarshal([]byte(text[l.end:lines[end].start]), &input)
		replies := []Reply{}
		valid := err == nil && len(input) > 0
		for _, q := range input {
			var answer string
			if string(q.Answer) == "null" || len(q.Answer) == 0 {
				valid = false
			} else if json.Unmarshal(q.Answer, &answer) != nil {
				var choices []json.RawMessage
				if json.Unmarshal(q.Answer, &choices) != nil {
					valid = false
				}
				values := make([]string, len(choices))
				for j, choice := range choices {
					if string(choice) == "null" || json.Unmarshal(choice, &values[j]) != nil {
						valid = false
					}
				}
				answer = strings.Join(values, "\n")
			}
			if strings.TrimSpace(q.Question) == "" {
				valid = false
			}
			replies = append(replies, Reply{q.Question, answer})
		}
		if valid {
			if cursor < l.start {
				parts = append(parts, Part{Text: text[cursor:l.start]})
			}
			parts = append(parts, Part{Replies: replies})
			cursor = lines[end].end
		}
		i = end
	}
	if cursor < len(text) {
		parts = append(parts, Part{Text: text[cursor:]})
	}
	return parts
}

// PlainText is the readable copy/preview representation, without internal IDs.
func PlainText(parts []Part) string {
	structured := false
	for _, part := range parts {
		structured = structured || len(part.Replies) > 0
	}
	var out []string
	for _, part := range parts {
		if part.Replies == nil {
			text := part.Text
			if structured {
				text = strings.TrimSpace(text)
			}
			if text != "" {
				out = append(out, text)
			}
		}
		for _, reply := range part.Replies {
			answer := reply.Answer
			if answer == "" {
				answer = "未填写"
			}
			out = append(out, "问题："+reply.Question+"\n回答："+answer)
		}
	}
	if structured {
		return strings.Join(out, "\n\n")
	}
	return strings.Join(out, "")
}
