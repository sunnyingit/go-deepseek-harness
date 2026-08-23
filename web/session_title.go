package main

import (
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"go-harness/core/session"
)

const sessionTitleMaxRunes = 36

// Matches wrapSkillForAgent in the UI: a load_skill preamble plus the
// original user task after "用户任务：".
var skillWrapRe = regexp.MustCompile(`(?s)^请先调用 load_skill 工具，参数 name 为 "[^"]+"。.*?\n用户任务：\n`)

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
		text := stripSkillWrap(strings.Join(parts, " "))
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

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
