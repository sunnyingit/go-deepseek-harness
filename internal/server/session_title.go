package server

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"go-harness/core/session"
)

const sessionTitleMaxRunes = 36

// scheduledAskTitlePrefix marks dialog sessions created by a custom 「定时提问」task.
const scheduledAskTitlePrefix = "【定时任务-模型提问】"

// sessionUpdatedAt is the newest log timestamp, so the sidebar can put the
// conversation that was just used above older ones. Creation time is the fallback.
func sessionUpdatedAt(sess *session.Session) int64 {
	if sess == nil {
		return 0
	}
	ev := sess.Events()
	if n := len(ev); n > 0 && ev[n-1].Time > 0 {
		return ev[n-1].Time
	}
	return sess.Header().CreatedAt
}

// Matches wrapSkillForAgent in the UI: a load_skill preamble plus the
// original user task after "用户任务：".
var skillWrapRe = regexp.MustCompile(`(?s)^请先调用 load_skill 工具，参数 name 为 "[^"]+"。.*?\n用户任务：\n`)

// askAnalystWrapRe matches dialog @-mention routing written into the user turn
// ("请调用 ask_analyst：analyst=…，question=…"). Sidebar titles must show the
// human question, not the orchestration cue.
var askAnalystWrapRe = regexp.MustCompile(`(?is)^请(?:调用|优先调用)\s*ask_analyst\s*[：:]\s*analyst=\w+[\s\S]*?(?:question\s*=|回答\s*[：:])\s*([\s\S]+)$`)

var (
	ctxBracketPrefixRe = regexp.MustCompile(`^【(?:带上|上下文)：[^】]*】\s*`)
	askAnalystTailRe   = regexp.MustCompile(`。本轮只请这一位[\s\S]*$`)
)

func sessionTitle(sess *session.Session) string {
	if sess == nil {
		return ""
	}
	var uploaded []string
	for _, m := range sess.DeriveMessages() {
		if m.Role != session.RoleUser {
			continue
		}
		var parts []string
		onlyToolResults := true
		for _, b := range m.Content {
			if b.Kind == session.BlockToolResult {
				continue
			}
			onlyToolResults = false
			if b.Kind != session.BlockText {
				continue
			}
			t := strings.TrimSpace(b.Text)
			if t == "" {
				continue
			}
			if isUploadNotice(t) {
				if name := uploadNoticeFilename(t); name != "" {
					uploaded = append(uploaded, name)
				}
				continue
			}
			parts = append(parts, t)
		}
		if onlyToolResults || len(parts) == 0 {
			continue
		}
		text := humanSessionTitleText(strings.Join(parts, " "))
		if strings.HasPrefix(strings.TrimSpace(text), scheduledAskTitlePrefix) {
			return scheduledAskTitle(text)
		}
		text = strings.Join(strings.Fields(text), " ")
		if text == "" {
			continue
		}
		return truncateRunes(text, sessionTitleMaxRunes)
	}
	if len(uploaded) == 0 {
		return ""
	}
	return truncateRunes(strings.Join(uploaded, "、"), sessionTitleMaxRunes)
}

// humanSessionTitleText strips orchestration wrappers so the sidebar shows what
// the user asked, matching prettyUserText in dailog.html.
func humanSessionTitleText(text string) string {
	t := strings.TrimSpace(text)
	t = stripSkillWrap(t)
	t = ctxBracketPrefixRe.ReplaceAllString(t, "")
	t = strings.TrimSpace(t)
	if m := askAnalystWrapRe.FindStringSubmatch(t); len(m) == 2 {
		t = strings.TrimSpace(m[1])
	}
	t = askAnalystTailRe.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}

// sessionQuestionCount is how many user-authored turns are on the surface.
// Tool-result-only user messages (observations) do not count — those are
// not questions the user asked. Upload-notice injections also do not count.
func sessionQuestionCount(sess *session.Session) int {
	if sess == nil {
		return 0
	}
	n := 0
	for _, m := range sess.DeriveMessages() {
		if m.Role != session.RoleUser {
			continue
		}
		hasText := false
		onlyToolResults := true
		for _, b := range m.Content {
			if b.Kind == session.BlockToolResult {
				continue
			}
			onlyToolResults = false
			if b.Kind == session.BlockText {
				t := strings.TrimSpace(b.Text)
				if t != "" && !isUploadNotice(t) {
					hasText = true
				}
			}
		}
		if !onlyToolResults && hasText {
			n++
		}
	}
	return n
}

func isUploadNotice(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "[Uploaded file]")
}

func uploadNoticeFilename(text string) string {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "[Uploaded file]"))
	if i := strings.Index(t, " ("); i > 0 {
		t = strings.TrimSpace(t[:i])
	}
	return filepath.Base(t)
}

func stripSkillWrap(text string) string {
	return strings.TrimSpace(skillWrapRe.ReplaceAllString(text, ""))
}

// scheduledAskTitle keeps the fixed prefix and shortens the first line (task · stock).
func scheduledAskTitle(text string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), scheduledAskTitlePrefix))
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = strings.TrimSpace(rest[:i])
	}
	rest = strings.Join(strings.Fields(rest), " ")
	if rest == "" {
		return scheduledAskTitlePrefix
	}
	return scheduledAskTitlePrefix + truncateRunes(rest, sessionTitleMaxRunes)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
