package hubui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/monkeyWie/reign/pkg/messagecontent"
)

type Directory struct {
	Path, Title string
	Sessions    []Object
}
type Message struct {
	ID, Role, Text, Turn string
	Live                 bool
	Timestamp            int64
	Attachments          []Object
	Tools                []Object
}

func directories(sessions []Object) []Directory {
	unique := map[string]Object{}
	preferred := func(s Object) bool {
		return str(s, "providerId") == "codex-app" && str(s, "sourceKind") == "app" || str(s, "providerId") == "codex-cli" && str(s, "sourceKind") == "cli"
	}
	for _, s := range sessions {
		p := str(s, "providerId")
		if p == "codex-vscode" || str(s, "sourceKind") == "vscode" {
			continue
		}
		family := p
		if strings.HasPrefix(p, "codex-") {
			family = "codex"
		}
		key := family + "|" + str(s, "id")
		old := unique[key]
		if old == nil || preferred(s) && !preferred(old) || preferred(s) == preferred(old) && number(s, "updatedAt") > number(old, "updatedAt") {
			unique[key] = s
		}
	}
	groups := map[string][]Object{}
	for _, s := range unique {
		path := str(s, "cwd")
		groups[path] = append(groups[path], s)
	}
	out := []Directory{}
	for path, items := range groups {
		sort.Slice(items, func(i, j int) bool {
			a, b := number(items[i], "updatedAt"), number(items[j], "updatedAt")
			if a != b {
				return a > b
			}
			return str(items[i], "providerId")+str(items[i], "id") < str(items[j], "providerId")+str(items[j], "id")
		})
		name := "无工作目录"
		if path != "" {
			parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
			name = path
			if len(parts) > 0 {
				name = parts[len(parts)-1]
			}
		}
		out = append(out, Directory{path, name, items})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := number(out[i].Sessions[0], "updatedAt"), number(out[j].Sessions[0], "updatedAt")
		if a != b {
			return a > b
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func activeTurn(t Object) Object {
	turns := objects(t["turns"])
	if len(turns) > 0 && str(turns[len(turns)-1], "status") == "inProgress" {
		return turns[len(turns)-1]
	}
	return nil
}
func visibleTool(item Object) bool {
	switch str(item, "type") {
	case "functionCallOutput", "hookPrompt", "contextCompaction", "enteredReviewMode", "exitedReviewMode":
		return false
	}
	return str(item, "role") == "tool"
}
func activityLabel(tools []Object) string {
	labels := map[string]string{"read": "读取文件", "list": "列出目录", "search": "搜索", "command": "运行命令", "edit": "编辑文件", "web": "浏览网页", "tool": "调用工具", "agent": "子 Agent", "image": "图片", "plan": "更新计划"}
	counts := map[string]int{}
	order := []string{}
	running := ""
	for _, tool := range tools {
		acts := objects(tool["activities"])
		if len(acts) == 0 {
			acts = []Object{{"kind": "tool", "title": "执行工具 " + str(tool, "type")}}
		}
		for _, a := range acts {
			kind := str(a, "kind")
			if counts[kind] == 0 {
				order = append(order, kind)
			}
			counts[kind]++
			if len(objects(tool["activities"])) > 0 && str(tool, "status") == "inProgress" {
				running = "正在" + str(a, "title")
			}
		}
	}
	if running != "" {
		return running
	}
	parts := []string{}
	for _, kind := range order {
		label := labels[kind]
		if label == "" {
			label = "活动"
		}
		parts = append(parts, fmt.Sprintf("%s %d", label, counts[kind]))
	}
	return strings.Join(parts, " · ")
}

func messages(t Object) []Message {
	out := []Message{}
	active := str(activeTurn(t), "id")
	for _, turn := range objects(t["turns"]) {
		id := str(turn, "id")
		live := id != "" && id == active
		tools := []Object{}
		flush := func() {
			if len(tools) > 0 {
				out = append(out, Message{ID: id + "|tools|" + str(tools[0], "id"), Role: "activity", Turn: id, Live: live, Tools: tools})
				tools = nil
			}
		}
		for _, item := range objects(turn["items"]) {
			role := str(item, "role")
			if visibleTool(item) && live {
				tools = append(tools, item)
				continue
			}
			if role != "user" && role != "assistant" {
				continue
			}
			if str(item, "phase") == "commentary" && !live {
				continue
			}
			body := str(item, "text")
			attachments := objects(item["attachments"])
			if strings.TrimSpace(body) == "" && len(attachments) == 0 {
				continue
			}
			flush()
			out = append(out, Message{ID: id + "|" + str(item, "id"), Role: role, Text: body, Turn: id, Live: live && role == "assistant", Timestamp: number(item, "timestampMs"), Attachments: attachments})
		}
		flush()
	}
	return out
}

var attachmentHeader = regexp.MustCompile(`(?m)^## [^\n]+?: (.+)$`)
var attachmentLink = regexp.MustCompile(`\]\(<([^>]+)>\)`)

func displayText(m Message) string {
	body := m.Text
	if m.Role != "user" || len(m.Attachments) == 0 {
		return body
	}
	sources := map[string]bool{}
	for _, a := range m.Attachments {
		sources[str(a, "source")] = true
	}
	const marker = "## My request:"
	if i := strings.Index(body, marker); i >= 0 && strings.HasPrefix(strings.TrimSpace(body), "# Files mentioned by the user:") {
		entries := attachmentHeader.FindAllStringSubmatch(body[:i], -1)
		valid := len(entries) > 0
		for _, e := range entries {
			valid = valid && sources[strings.TrimSpace(e[1])]
		}
		if valid {
			body = strings.TrimSpace(body[i+len(marker):])
		}
	}
	const upload = "RAS 上传的附件（已保存在本机，可按需读取）："
	if i := strings.Index(body, upload); i >= 0 {
		entries := attachmentLink.FindAllStringSubmatch(body[i:], -1)
		valid := len(entries) > 0
		for _, e := range entries {
			valid = valid && sources[e[1]]
		}
		if valid {
			body = strings.TrimSpace(body[:i])
		}
	}
	return body
}

func messageCopyText(m Message) string {
	if m.Role == "user" {
		return messagecontent.PlainText(messagecontent.Parse(m.Text))
	}
	return m.Text
}

func messagePreviewText(m Message) string {
	text := displayText(m)
	if m.Role == "user" {
		return messagecontent.PlainText(messagecontent.Parse(text))
	}
	return text
}

func mergeLatest(old, latest Object) Object {
	out := copyObject(latest)
	previous, next := objects(old["turns"]), objects(latest["turns"])
	anchor := -1
	if len(next) > 0 {
		for i, t := range previous {
			if str(t, "id") == str(next[0], "id") {
				anchor = i
				break
			}
		}
	}
	if anchor > 0 {
		seen := map[string]bool{}
		for _, t := range next {
			seen[str(t, "id")] = true
		}
		kept := []Object{}
		for _, t := range previous[:anchor] {
			if !seen[str(t, "id")] {
				kept = append(kept, t)
			}
		}
		out["turns"] = append(kept, next...)
		out["nextCursor"] = old["nextCursor"]
	}
	return out
}

// Event offsets are UTF-16, matching Codex and the existing Flutter client.
// Snapshots can already contain an event; overlap must be idempotent.
func applyEvents(source Object, events []Object) (Object, bool) {
	encoded, _ := json.Marshal(source)
	out := Object{}
	_ = json.Unmarshal(encoded, &out)
	turns := objects(out["turns"])
	reset := false
	for _, e := range events {
		kind := str(e, "kind")
		if kind == "snapshot" {
			reset = true
			continue
		}
		id := str(e, "turnId")
		if id == "" {
			continue
		}
		var turn Object
		for _, t := range turns {
			if str(t, "id") == id {
				turn = t
				break
			}
		}
		if turn == nil {
			turn = Object{"id": id, "status": "inProgress", "items": []Object{}}
			turns = append(turns, turn)
		}
		terminal := str(turn, "status") == "completed" || str(turn, "status") == "failed" || str(turn, "status") == "interrupted"
		if kind == "turn" {
			if !terminal || str(e, "status") != "inProgress" {
				turn["status"] = e["status"]
			}
			if str(e, "status") != "inProgress" {
				reset = true
			}
			continue
		}
		items := objects(turn["items"])
		index := -1
		for i, v := range items {
			if str(v, "id") == str(e, "itemId") {
				index = i
				break
			}
		}
		old := Object{}
		if index >= 0 {
			old = items[index]
		}
		next := copyObject(old)
		switch kind {
		case "item":
			for k, v := range object(e["item"]) {
				next[k] = v
			}
			if strings.HasPrefix(str(old, "text"), str(next, "text")) {
				next["text"] = old["text"]
			}
			if terminal || str(old, "status") == "completed" && str(next, "status") == "inProgress" {
				next["status"] = old["status"]
			}
		case "summary":
			if terminal {
				continue
			}
			next["id"] = e["itemId"]
			next["role"] = "reasoning"
			next["reasoningSummary"] = e["summary"]
		case "text":
			if terminal {
				continue
			}
			s, d := utf16.Encode([]rune(str(old, "text"))), utf16.Encode([]rune(str(e, "delta")))
			offset := int(number(e, "offset"))
			if offset < 0 || offset > len(s) {
				reset = true
				continue
			}
			overlap := min(len(s)-offset, len(d))
			if fmt.Sprint(s[offset:offset+overlap]) != fmt.Sprint(d[:overlap]) {
				reset = true
				continue
			}
			next["id"] = e["itemId"]
			if str(next, "role") == "" {
				next["role"] = "assistant"
			}
			next["text"] = string(utf16.Decode(append(s, d[overlap:]...)))
		default:
			continue
		}
		if index < 0 {
			items = append(items, next)
		} else {
			items[index] = next
		}
		turn["items"] = items
	}
	out["turns"] = turns
	return out, reset
}
