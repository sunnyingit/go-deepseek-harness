package main

import "go-harness/core/session"

// messageDTO/blockDTO are this server's own wire shapes — deliberately not
// the same as session.Message/ContentBlock. A web API's JSON contract and a
// package's internal Go types are different concerns; changing one must
// never silently change the other.
type messageDTO struct {
	Role    string     `json:"role"`
	Content []blockDTO `json:"content"`
}

type blockDTO struct {
	Kind     string         `json:"kind"`
	Text     string         `json:"text,omitempty"`
	CallID   string         `json:"callId,omitempty"`
	ToolName string         `json:"toolName,omitempty"`
	Args     map[string]any `json:"args,omitempty"`
}

func toMessageDTO(m session.Message) messageDTO {
	blocks := make([]blockDTO, len(m.Content))
	for i, b := range m.Content {
		blocks[i] = blockDTO{Kind: string(b.Kind), Text: b.Text, CallID: b.CallID, ToolName: b.ToolName, Args: b.Args}
	}
	return messageDTO{Role: string(m.Role), Content: blocks}
}

func toMessageDTOs(msgs []session.Message) []messageDTO {
	out := make([]messageDTO, len(msgs))
	for i, m := range msgs {
		out[i] = toMessageDTO(m)
	}
	return out
}

type skillDTO struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Provider       string `json:"provider"`
	ModelInvocable bool   `json:"modelInvocable"`
	UserInvocable  bool   `json:"userInvocable"`
}

type sessionDTO struct {
	ID             string `json:"id"`
	CWD            string `json:"cwd"`
	ParentSession  string `json:"parentSession,omitempty"`
	PlanModeActive bool   `json:"planModeActive"`
	Status         string `json:"status"`
}
