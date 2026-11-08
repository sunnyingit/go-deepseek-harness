package inbox

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Quiet is a stretch of the day when normal notifications wait. Urgent ones (a price alert) still come
// through unless Urgent is false.
type Quiet struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Urgent bool   `json:"urgent"`
}

// Channel is somewhere a notification can be sent: the desktop, or an outside service.
type Channel struct {
	ID          string            `json:"channel_id"`
	Type        string            `json:"type"` // desktop | wecom_webhook | bark | serverchan
	Name        string            `json:"name"`
	Enabled     bool              `json:"enabled"`
	MinSeverity string            `json:"min_severity"`
	Secret      string            `json:"secret,omitempty"` // webhook address or push key; kept on this machine only
	Config      map[string]string `json:"config,omitempty"`
	Quiet       *Quiet            `json:"quiet_hours,omitempty"`
	Relay       bool              `json:"relay"` // also give it to the server so it can push while this computer is off
}

// TypeSetting is the user's choice for one kind of event.
type TypeSetting struct {
	Enabled  *bool    `json:"enabled,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

// Settings decide who gets bothered, where and when.
type Settings struct {
	Enabled   bool                   `json:"enabled"`
	Quiet     Quiet                  `json:"quiet_hours"`
	CollapseS int                    `json:"collapse_s"`
	Digest    struct{ Enabled bool } `json:"digest"`
	Channels  []Channel              `json:"channels"`
	Types     map[string]TypeSetting `json:"types"`
}

func defaultSettings() *Settings {
	s := &Settings{Enabled: true, Quiet: Quiet{From: "22:00", To: "08:00", Urgent: true}, CollapseS: 600, Types: map[string]TypeSetting{}}
	s.Channels = []Channel{{ID: "desktop", Type: "desktop", Name: "桌面通知", Enabled: true, MinSeverity: "normal"}}
	return s
}

func (in *Inbox) settings() *Settings {
	if in.Config == nil {
		in.Config = defaultSettings()
	}
	return in.Config
}

// Settings returns a copy.
func (in *Inbox) Settings() Settings {
	in.mu.Lock()
	defer in.mu.Unlock()
	s := *in.settings()
	s.Channels = append([]Channel(nil), s.Channels...)
	types := map[string]TypeSetting{}
	for k, v := range s.Types {
		types[k] = v
	}
	s.Types = types
	return s
}

func validClock(v string) bool {
	_, err := time.Parse("15:04", v)
	return err == nil
}

// SettingsPatch changes some of the settings; nil fields stay as they are.
type SettingsPatch struct {
	Enabled   *bool                   `json:"enabled"`
	Quiet     *Quiet                  `json:"quiet_hours"`
	CollapseS *int                    `json:"collapse_s"`
	Digest    *struct{ Enabled bool } `json:"digest"`
	Types     map[string]TypeSetting  `json:"types"`
}

func (in *Inbox) Update(p SettingsPatch) (Settings, error) {
	in.mu.Lock()
	s := in.settings()
	if p.Quiet != nil {
		if (p.Quiet.From != "" || p.Quiet.To != "") && (!validClock(p.Quiet.From) || !validClock(p.Quiet.To)) {
			in.mu.Unlock()
			return Settings{}, errors.New("免打扰时间要写成 22:00 这样")
		}
		s.Quiet = *p.Quiet
	}
	if p.CollapseS != nil {
		if *p.CollapseS < 0 || *p.CollapseS > 86400 {
			in.mu.Unlock()
			return Settings{}, errors.New("折叠窗口要在 0 到 86400 秒之间")
		}
		s.CollapseS = *p.CollapseS
	}
	if p.Enabled != nil {
		s.Enabled = *p.Enabled
	}
	if p.Digest != nil {
		s.Digest.Enabled = p.Digest.Enabled
	}
	for t, v := range p.Types {
		if _, ok := Registry[t]; !ok {
			in.mu.Unlock()
			return Settings{}, fmt.Errorf("未知的事件类型：%s", t)
		}
		s.Types[t] = v
	}
	in.save()
	in.mu.Unlock()
	return in.Settings(), nil
}

var channelTypes = map[string]bool{"desktop": true, "wecom_webhook": true, "bark": true, "serverchan": true}

// PutChannel creates or changes a channel. An empty Secret keeps the saved one.
func (in *Inbox) PutChannel(c Channel) (Channel, error) {
	c.ID = strings.TrimSpace(c.ID)
	if c.ID == "" || !channelTypes[c.Type] {
		return Channel{}, errors.New("渠道类型要是 desktop、wecom_webhook、bark 或 serverchan")
	}
	if c.MinSeverity == "" {
		c.MinSeverity = "normal"
	}
	if rank(c.MinSeverity) < 0 {
		return Channel{}, errors.New("min_severity 要是 info、normal 或 urgent")
	}
	if c.Quiet != nil && ((c.Quiet.From != "" || c.Quiet.To != "") && (!validClock(c.Quiet.From) || !validClock(c.Quiet.To))) {
		return Channel{}, errors.New("免打扰时间要写成 22:00 这样")
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	s := in.settings()
	for i := range s.Channels {
		if s.Channels[i].ID == c.ID {
			if c.Secret == "" {
				c.Secret = s.Channels[i].Secret
			}
			s.Channels[i] = c
			in.save()
			return c, nil
		}
	}
	if c.Name == "" {
		c.Name = c.ID
	}
	s.Channels = append(s.Channels, c)
	in.save()
	return c, nil
}

func (in *Inbox) DeleteChannel(id string) bool {
	if id == "desktop" {
		return false
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	s := in.settings()
	for i := range s.Channels {
		if s.Channels[i].ID == id {
			s.Channels = append(s.Channels[:i], s.Channels[i+1:]...)
			in.save()
			return true
		}
	}
	return false
}

// Channel returns one channel with its secret (for sending); ok is false when there is none.
func (in *Inbox) Channel(id string) (Channel, bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	for _, c := range in.settings().Channels {
		if c.ID == id {
			return c, true
		}
	}
	return Channel{}, false
}

// View hides the secret.
func (c Channel) View() map[string]any {
	return map[string]any{"channel_id": c.ID, "type": c.Type, "name": c.Name, "enabled": c.Enabled, "min_severity": c.MinSeverity, "has_secret": c.Secret != "",
		"config": c.Config, "quiet_hours": c.Quiet, "relay": c.Relay}
}
