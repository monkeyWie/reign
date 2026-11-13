package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/monkeyWie/reign/internal/assets"
	"github.com/monkeyWie/reign/internal/provider"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

var fileMention = regexp.MustCompile(`(?m)^## [^\n]+?: (.+)$`)
var fileLineSuffix = regexp.MustCompile(`:[0-9]+(?::[0-9]+)?$`)
var fileLineFragment = regexp.MustCompile(`^L[0-9]+(?:C[0-9]+)?(?:-L?[0-9]+(?:C[0-9]+)?)?$`)

func (p *Provider) providerID() string {
	if p.kind == "cli" {
		return "codex-cli"
	}
	return "codex-app"
}

func (p *Provider) attachments(ctx context.Context, session, cwd string, turn *provider.Turn, raw json.RawMessage) {
	var native struct {
		Items []struct {
			ID, Type, SavedPath string
			Content             json.RawMessage
		}
	}
	if json.Unmarshal(raw, &native) != nil {
		return
	}
	byID := map[string]int{}
	for i := range turn.Items {
		byID[turn.Items[i].ID] = i
	}
	scope := assets.Scope(p.providerID(), session)
	for _, n := range native.Items {
		idx, ok := byID[n.ID]
		if !ok {
			continue
		}
		item := &turn.Items[idx]
		add := func(ref, source string, inline bool) {
			if ctx.Err() != nil || len(item.Attachments) >= 32 {
				return
			}
			if strings.HasPrefix(source, "file://") {
				if u, e := url.Parse(source); e == nil && (u.Host == "" || u.Host == "localhost") {
					source = u.Path
				}
			}
			item.Attachments = append(item.Attachments, p.assets.Import(ctx, scope, turn.ID+"/"+n.ID+"/"+ref, source, inline))
		}
		if n.Type == "userMessage" {
			var contents []struct{ Type, Text, Path, URL, FileID string }
			if json.Unmarshal(n.Content, &contents) != nil {
				continue
			}
			var body strings.Builder
			for i, content := range contents {
				switch content.Type {
				case "text":
					if content.Text != "" {
						body.WriteString(content.Text)
						body.WriteByte('\n')
					}
				case "localImage":
					if content.Path != "" {
						add(fmt.Sprintf("input-%d", i), content.Path, false)
					}
				case "image":
					if strings.HasPrefix(content.URL, "data:image/") {
						add(fmt.Sprintf("input-%d", i), content.URL, false)
					} else {
						// Remote opaque file IDs have no local bytes to export.
						item.Attachments = append(item.Attachments, provider.Attachment{ID: "external-" + n.ID + fmt.Sprint(i), Name: "图片", Kind: "image", State: "unavailable"})
					}
				}
			}
			item.Text = body.String()
			// Desktop attachments can be represented in the recorded text preamble
			// rather than a native localImage input. Only recognize that exact envelope.
			if header, _, ok := strings.Cut(strings.TrimLeft(item.Text, "\r\n "), "## My request:"); ok && strings.HasPrefix(header, "# Files mentioned by the user:") {
				for i, entry := range fileMention.FindAllStringSubmatch(header, 32) {
					path := strings.TrimSpace(entry[1])
					if !filepath.IsAbs(path) {
						continue
					}
					duplicate := false
					for _, a := range item.Attachments {
						if a.Source == path {
							duplicate = true
							break
						}
					}
					if !duplicate {
						add(fmt.Sprintf("mentioned-%d", i), path, false)
					}
				}
			}
		}
		if n.Type == "imageGeneration" && n.SavedPath != "" {
			add("generated", n.SavedPath, false)
			item.Role, item.Text = "assistant", ""
		}
		if (item.Role != "assistant" && item.Role != "user") || len(item.Text) > 1<<20 {
			continue
		}
		root := goldmark.DefaultParser().Parse(text.NewReader([]byte(item.Text)))
		_ = ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
			if !entering {
				return ast.WalkContinue, nil
			}
			var source string
			inline := false
			switch link := node.(type) {
			case *ast.Image:
				source, inline = string(link.Destination), true
			case *ast.Link:
				source = string(link.Destination)
			default:
				return ast.WalkContinue, nil
			}
			u, e := url.Parse(source)
			if e != nil || (u.Scheme != "" && u.Scheme != "file") || u.Host != "" {
				return ast.WalkContinue, nil
			}
			path := u.Path
			if path == "" || strings.HasPrefix(path, "#") {
				return ast.WalkContinue, nil
			}
			// Codex source-location links stay in the message as navigation,
			// rather than becoming downloadable attachments (or missing files
			// such as README.md:3). This applies to links, not inline images.
			if !inline && (fileLineSuffix.MatchString(path) || fileLineFragment.MatchString(u.Fragment)) {
				return ast.WalkContinue, nil
			}
			if !filepath.IsAbs(path) {
				path = filepath.Join(cwd, path)
			}
			// Only export Markdown file references beneath this workspace or the
			// Hub attachment directory, not arbitrary paths sent by a controller.
			if !beneath(cwd, path) && !beneath(p.assets.Root, path) {
				return ast.WalkContinue, nil
			}
			for _, a := range item.Attachments {
				if a.Source == path || a.Source == source {
					return ast.WalkContinue, nil
				}
			}
			a := p.assets.Import(ctx, scope, turn.ID+"/"+n.ID+"/link/"+source, path, inline)
			a.Source = source
			item.Attachments = append(item.Attachments, a)
			if len(item.Attachments) >= 32 {
				return ast.WalkStop, nil
			}
			return ast.WalkContinue, nil
		})
	}
}

func beneath(root, path string) bool {
	if root == "" {
		return false
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	// Also check resolved targets when present, so a workspace symlink cannot
	// turn a Markdown attachment link into an arbitrary file export.
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return true
	}
	resolvedRoot, e := filepath.EvalSymlinks(root)
	if e != nil {
		return false
	}
	rel, e = filepath.Rel(resolvedRoot, resolved)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func (p *Provider) attachmentInput(session, message string, ids []string) ([]any, error) {
	if len(ids) > 16 {
		return nil, fmt.Errorf("每条消息最多 16 个附件")
	}
	input := []any{}
	if strings.TrimSpace(message) != "" {
		input = append(input, map[string]string{"type": "text", "text": message})
	}
	files := []string{}
	for _, id := range ids {
		meta, path, e := p.assets.Input(assets.Scope(p.providerID(), session), id)
		if e != nil {
			return nil, e
		}
		if meta.Kind == "image" {
			input = append(input, map[string]string{"type": "localImage", "path": path})
		} else {
			// Codex has native image input; ordinary files are local references
			// that the Agent can read with its normal filesystem tools.
			files = append(files, fmt.Sprintf("- [%s](<%s>)", strings.ReplaceAll(meta.Name, "]", "\\]"), filepath.ToSlash(path)))
		}
	}
	if len(files) > 0 {
		input = append(input, map[string]string{"type": "text", "text": "\nAgent Hub 上传的附件（已保存在本机，可按需读取）：\n" + strings.Join(files, "\n")})
	}
	if len(input) == 0 {
		return nil, fmt.Errorf("消息不能为空")
	}
	return input, nil
}

func (p *Provider) SendAttachments(ctx context.Context, session, message, model string, ids []string) error {
	input, e := p.attachmentInput(session, message, ids)
	if e != nil {
		return e
	}
	var resumed json.RawMessage
	if e = p.call(ctx, "thread/resume", map[string]any{"threadId": session, "excludeTurns": true}, &resumed); e != nil {
		return fmt.Errorf("无法接续原会话（不会另建会话）: %w", e)
	}
	params := map[string]any{"threadId": session, "input": input}
	if model != "" {
		params["model"] = model
	}
	return p.call(ctx, "turn/start", params, nil)
}
func (p *Provider) SteerAttachments(ctx context.Context, session, turn, message string, ids []string) error {
	input, e := p.attachmentInput(session, message, ids)
	if e != nil {
		return e
	}
	return p.call(ctx, "turn/steer", map[string]any{"threadId": session, "expectedTurnId": turn, "input": input}, nil)
}
