package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var sent sync.Map

// Emit posts one markdown message to an enterprise WeChat group robot.
// The same key is sent at most once per calendar day. An empty webhook
// returns "unconfigured" and does not count as sent.
func Emit(webhook, key, title, text string) string {
	webhook = strings.TrimSpace(webhook)
	if webhook == "" {
		return "unconfigured"
	}
	full := time.Now().Format("2006-01-02") + ":" + key
	if _, loaded := sent.LoadOrStore(full, true); loaded {
		return "duplicate"
	}
	content := "**" + title + "**\n" + clip(text, 500)
	if err := post(webhook, content); err != nil {
		sent.Delete(full)
		return "failed"
	}
	return "sent"
}

func post(webhook, content string) error {
	body, err := json.Marshal(map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"content": content},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wecom %d", resp.StatusCode)
	}
	var payload struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.ErrCode != 0 {
		return fmt.Errorf("%s", payload.ErrMsg)
	}
	return nil
}

func clip(text string, n int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n]) + "…"
}
