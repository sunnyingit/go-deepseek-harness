package main

import (
	"strings"
	"testing"

	"go-harness/core/session"
)

func TestSessionTitleUsesFirstUserQuestion(t *testing.T) {
	s := session.New("s1", session.Header{})
	s.Append("user/message", session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockText, Text: "帮我写一份本周周报"}},
	}, session.WithSurfaceOp(session.AppendOp()))
	got := sessionTitle(s)
	if got != "帮我写一份本周周报" {
		t.Fatalf("title = %q", got)
	}
}

func TestSessionTitleStripsSkillWrapAndTruncates(t *testing.T) {
	s := session.New("s1", session.Header{})
	wrapped := "请先调用 load_skill 工具，参数 name 为 \"pptx\"。必须先加载该技能并严格按其指令完成任务，不要跳过 load_skill。\n\n用户任务：\n" +
		"根据这份材料做一版面向投资人的融资路演PPT，要把市场、产品、团队和财务全部讲清楚"
	s.Append("user/message", session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockText, Text: wrapped}},
	}, session.WithSurfaceOp(session.AppendOp()))
	got := sessionTitle(s)
	if strings.Contains(got, "load_skill") {
		t.Fatalf("skill wrap leaked into title: %q", got)
	}
	if !strings.HasPrefix(got, "根据这份材料") {
		t.Fatalf("title = %q, want the user task", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("long title should be truncated: %q", got)
	}
}

func TestSessionTitleSkipsToolResultOnlyUserTurns(t *testing.T) {
	s := session.New("s1", session.Header{})
	s.Append("tool/result", map[string]any{"message": session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockToolResult, Text: "ok"}},
	}}, session.WithSurfaceOp(session.AppendOp()))
	s.Append("user/message", session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockText, Text: "真正的问题"}},
	}, session.WithSurfaceOp(session.AppendOp()))
	if got := sessionTitle(s); got != "真正的问题" {
		t.Fatalf("title = %q", got)
	}
}

func TestSessionQuestionCountSkipsToolResults(t *testing.T) {
	s := session.New("s1", session.Header{})
	s.Append("user/message", session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockText, Text: "第一问"}},
	}, session.WithSurfaceOp(session.AppendOp()))
	s.Append("tool/result", map[string]any{"message": session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockToolResult, CallID: "c1", Text: "ok"}},
	}}, session.WithSurfaceOp(session.AppendOp()))
	s.Append("user/message", session.Message{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Kind: session.BlockText, Text: "第二问"}},
	}, session.WithSurfaceOp(session.AppendOp()))
	if got := sessionQuestionCount(s); got != 2 {
		t.Fatalf("questionCount = %d, want 2", got)
	}
}

func TestSessionTitleEmptySession(t *testing.T) {
	if got := sessionTitle(session.New("s1", session.Header{})); got != "" {
		t.Fatalf("empty session title = %q", got)
	}
}
