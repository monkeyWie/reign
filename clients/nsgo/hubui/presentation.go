package hubui

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/monkeyWie/reignhub/pkg/messagecontent"
)

type Directory struct {
	Key, Path, Title string
	Sessions         []Object
}
type Message struct {
	ID, Role, Text, Turn    string
	Delivery, DeliveryError string
	Live                    bool
	Timestamp               int64
	Attachments             []Object
	Tools                   []Object
}

func directories(sessions []Object) []Directory {
	unique := map[string]Object{}
	preferred := func(s Object) bool {
		return str(s, "providerId") == "codex-app" && str(s, "sourceKind") == "app" || str(s, "providerId") == "codex-cli" && str(s, "sourceKind") == "cli"
	}
	for _, s := range sessions {
		p := str(s, "providerId")
		if !visibleSession(s) || str(s, "sourceKind") == "vscode" {
			continue
		}
		family := agentFamily(p)
		key := family + "|" + str(s, "id")
		old := unique[key]
		if old == nil || preferred(s) && !preferred(old) || preferred(s) == preferred(old) && number(s, "updatedAt") > number(old, "updatedAt") {
			unique[key] = s
		}
	}
	ordered := make([]Object, 0, len(unique))
	for _, session := range unique {
		ordered = append(ordered, session)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if a, b := number(ordered[i], "updatedAt"), number(ordered[j], "updatedAt"); a != b {
			return a > b
		}
		return str(ordered[i], "providerId")+"|"+str(ordered[i], "id") < str(ordered[j], "providerId")+"|"+str(ordered[j], "id")
	})
	groups := map[string]*Directory{}
	titled := map[string]bool{}
	for _, s := range ordered {
		directory := projectDirectory(s)
		group := groups[directory.Key]
		if group == nil {
			group = &directory
			groups[directory.Key] = group
		}
		if group.Path == "" && directory.Path != "" {
			group.Path = directory.Path
		}
		if str(object(s["project"]), "name") != "" && !titled[directory.Key] {
			group.Title = directory.Title
			titled[directory.Key] = true
		}
		group.Sessions = append(group.Sessions, s)
	}
	out := []Directory{}
	for _, group := range groups {
		items := group.Sessions
		sort.Slice(items, func(i, j int) bool {
			a, b := number(items[i], "updatedAt"), number(items[j], "updatedAt")
			if a != b {
				return a > b
			}
			return str(items[i], "providerId")+str(items[i], "id") < str(items[j], "providerId")+str(items[j], "id")
		})
		// Keep an executable directory independently of the normalized group key.
		if group.Path == "" {
			group.Path = str(items[0], "cwd")
		}
		out = append(out, *group)
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := number(out[i].Sessions[0], "updatedAt"), number(out[j].Sessions[0], "updatedAt")
		if a != b {
			return a > b
		}
		return out[i].Key < out[j].Key
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
	// Older cached transcripts may contain a provider's synthetic image marker.
	// Remove only trailing markers backed by actual image attachments.
	for _, a := range m.Attachments {
		if str(a, "kind") == "image" {
			trimmed := strings.TrimSpace(body)
			for _, marker := range []string{"[localImage]", "[image]"} {
				if strings.HasSuffix(trimmed, marker) {
					body = strings.TrimSpace(strings.TrimSuffix(trimmed, marker))
					break
				}
			}
		}
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
	for _, upload := range []string{
		"ReignHub 上传的附件（已保存在本机，可按需读取）：",
		"Agent Hub 上传的附件（已保存在本机，可按需读取）：",
		"RAS 上传的附件（已保存在本机，可按需读取）：",
	} {
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
	}
	return body
}

func messageCopyText(m Message) string {
	if m.Role == "user" {
		return messagecontent.PlainText(messagecontent.Parse(displayText(m)))
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
	previous, next := objects(old["turns"]), append([]Object{}, objects(latest["turns"])...)
	// A recovered connection can first return its last persisted snapshot.
	// Preserve the live tail until a newer/final snapshot actually supersedes it.
	if activeTurn(old) != nil {
		if len(next) == 0 {
			out["turns"], out["nextCursor"] = previous, old["nextCursor"]
			return out
		}
		for i, turn := range previous {
			if str(turn, "id") == str(next[len(next)-1], "id") && i < len(previous)-1 {
				next = append(append([]Object{}, next...), previous[i+1:]...)
				break
			}
		}
	}
	byID := map[string]Object{}
	for _, turn := range previous {
		byID[str(turn, "id")] = turn
	}
	for i, turn := range next {
		prior := byID[str(turn, "id")]
		if prior == nil || str(turn, "status") != "inProgress" {
			continue // A final snapshot is authoritative, including corrected text.
		}
		if str(prior, "status") != "inProgress" {
			next[i] = prior
			continue
		}
		merged := copyObject(turn)
		items := append([]Object{}, objects(prior["items"])...)
		indices := map[string]int{}
		for j, item := range items {
			indices[str(item, "id")] = j
		}
		for _, item := range objects(turn["items"]) {
			j, exists := indices[str(item, "id")]
			if !exists {
				indices[str(item, "id")] = len(items)
				items = append(items, item)
				continue
			}
			value := copyObject(item)
			if text := str(items[j], "text"); strings.HasPrefix(text, str(item, "text")) {
				value["text"] = text
			}
			if str(items[j], "status") == "completed" && str(item, "status") == "inProgress" {
				value["status"] = "completed"
			}
			items[j] = value
		}
		merged["items"] = items
		next[i] = merged
	}
	out["turns"] = next
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

// Event offsets are UTF-16, matching Codex and the shared event protocol.
// Snapshots can already contain an event; overlap must be idempotent.
func applyEvents(source Object, events []Object) (Object, bool) {
	if len(events) == 0 {
		return source, false
	}
	// Copy only the paths changed by this batch. Serializing the entire history
	// for every live delta blocks the UI loop on large conversations.
	out := copyObject(source)
	turns := append([]Object(nil), objects(source["turns"])...)
	indices := make(map[string]int, len(turns))
	for i, turn := range turns {
		indices[str(turn, "id")] = i
	}
	editable := map[string]bool{}
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
		i, exists := indices[id]
		if !exists {
			i = len(turns)
			indices[id] = i
			turns = append(turns, Object{"id": id, "status": "inProgress", "items": []Object{}})
		}
		if !editable[id] {
			turns[i] = copyObject(turns[i])
			turns[i]["items"] = append([]Object(nil), objects(turns[i]["items"])...)
			editable[id] = true
		}
		turn := turns[i]
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
