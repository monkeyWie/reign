package codex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monkeyWie/reignhub/internal/provider"
)

const asyncQuestionMethod = "question/async"
const questionReplyOpen = "<send_user_message_question_reply>"
const questionReplyClose = "</send_user_message_question_reply>"

type asyncQuestionReply struct {
	QuestionItemID string `json:"questionItemId"`
	Question       string `json:"question"`
	Answer         string `json:"answer"`
}

func validateQuestionDecision(a provider.Approval, d provider.Decision) error {
	if d.Decision == "skip" && a.AllowSkip && len(d.Answers) == 0 {
		return nil
	}
	if d.Decision != "input" {
		return errors.New("请选择或填写回答")
	}
	known := map[string]bool{}
	for _, q := range a.Questions {
		known[q.ID] = true
		answer := d.Answers[q.ID].Answers
		if len(answer) > 1 || (!a.AllowSkip && (len(answer) != 1 || strings.TrimSpace(answer[0]) == "")) {
			return errors.New("请回答所有问题")
		}
	}
	count := 0
	for id, answer := range d.Answers {
		if !known[id] || len(answer.Answers) != 1 || strings.TrimSpace(answer.Answers[0]) == "" {
			return errors.New("问题已变化，请重新选择回答")
		}
		count++
	}
	if count == 0 {
		return errors.New("请选择或填写回答")
	}
	return nil
}

// Mid-turn async prompts are agentMessage metadata, not app-server requests.
// Only the latest turn is actionable. A new turn expires its predecessor's
// unanswered prompts; an accepted reply hides exactly the matching question ID.
func asyncQuestionRequests(id string, turns []map[string]any, dismissed map[string]bool) []any {
	if len(turns) == 0 {
		return nil
	}
	turn := turns[len(turns)-1]
	if turn["status"] != "inProgress" && turn["status"] != "completed" {
		return nil
	}
	items := array(turn["items"])
	answered := map[string]bool{}
	for _, raw := range items {
		i := object(raw)
		content := i["content"]
		if i["type"] == "steeringUserMessage" && (i["status"] == "accepted" || i["steerAcknowledged"] == true) {
			content = i["input"]
		} else if i["type"] != "userMessage" {
			continue
		}
		text := []string{}
		for _, value := range array(content) {
			if c := object(value); c["type"] == "text" {
				text = append(text, stringValue(c["text"]))
			}
		}
		value := strings.TrimSpace(strings.Join(text, "\n"))
		if !strings.HasPrefix(value, questionReplyOpen) || !strings.HasSuffix(value, questionReplyClose) {
			continue
		}
		var replies []asyncQuestionReply
		if json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(value, questionReplyOpen), questionReplyClose)), &replies) == nil {
			for _, reply := range replies {
				if strings.TrimSpace(reply.Answer) != "" {
					answered[reply.QuestionItemID] = true
				}
			}
		}
	}
	requests := []any{}
	for _, raw := range items {
		i := object(raw)
		itemID := stringValue(i["id"])
		if i["type"] != "agentMessage" || itemID == "" {
			continue
		}
		questions := []provider.Question{}
		for index, raw := range array(i["questions"]) {
			q := object(raw)
			id := string(rawJSON([]any{"request_user_input_async", itemID, index}))
			if answered[id] || dismissed[id] || strings.TrimSpace(stringValue(q["title"])) == "" {
				continue
			}
			question := provider.Question{ID: id, Question: stringValue(q["title"])}
			for _, option := range array(q["options"]) {
				if value := stringValue(option); value != "" {
					question.Options = append(question.Options, provider.QuestionOption{Label: value})
				}
			}
			questions = append(questions, question)
		}
		if len(questions) == 0 {
			continue
		}
		params := map[string]any{"threadId": id, "turnId": turn["id"], "questions": questions}
		// Updating/answering part of a form invalidates stale drafts and decisions.
		key := fmt.Sprintf("async:%s:%s:%x", stringValue(turn["id"]), itemID, sha256.Sum256(rawJSON(questions)))
		requests = append(requests, map[string]any{"method": asyncQuestionMethod, "requestId": key, "params": decodeObject(rawJSON(params))})
	}
	return requests
}

func (p *Provider) dismissedQuestions(id, owner, except string) map[string]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	dismissed := map[string]bool{}
	for _, entry := range p.pending {
		if entry.claimed && entry.route == "ipc:"+id+":"+owner && entry.requestID != except && entry.approval.Method == asyncQuestionMethod {
			for _, q := range entry.approval.Questions {
				dismissed[q.ID] = true
			}
		}
	}
	return dismissed
}

func (b *appIPC) answerAsyncQuestions(ctx context.Context, id, owner string, request map[string]any, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := b.waitState(ctx, id, owner)
	if err != nil {
		return err
	}
	state := decodeObject(raw)
	current := false
	turns, _, err := stateTurns(state)
	if err != nil {
		return err
	}
	for _, value := range asyncQuestionRequests(id, turns, b.p.dismissedQuestions(id, owner, string(rawJSON(request["requestId"])))) {
		if object(value)["requestId"] == request["requestId"] {
			current = true
			break
		}
	}
	if !current {
		return errors.New("问题已回答或已过期，请刷新会话")
	}
	var input struct{ Questions []provider.Question }
	var decision provider.Decision
	_ = json.Unmarshal(rawJSON(request["params"]), &input)
	_ = json.Unmarshal(rawJSON(result), &decision)
	replies := []asyncQuestionReply{}
	for _, q := range input.Questions {
		if answer := decision.Answers[q.ID].Answers; len(answer) == 1 {
			replies = append(replies, asyncQuestionReply{q.ID, q.Question, answer[0]})
		}
	}
	text := questionReplyOpen + "\n" + string(rawJSON(replies)) + "\n" + questionReplyClose
	message := ipcInput([]any{map[string]any{"type": "text", "text": text}})
	method := "thread-follower-start-turn"
	params := map[string]any{"conversationId": id, "turnStart": map[string]any{"request": ipcTurnStart(state, id, message, ""), "context": map[string]any{"useAppServerPermissionDefault": true}}}
	if turns[len(turns)-1]["status"] == "inProgress" {
		method = "thread-follower-steer-turn"
		params = ipcSteerParams(state, id, message)
	}
	_, err = b.request(ctx, method, params, owner)
	if err == nil {
		b.markDirty(id)
	}
	return ipcWriteError(method, err)
}
