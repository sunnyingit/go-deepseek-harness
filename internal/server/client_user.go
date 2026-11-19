package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 本机客户端身份：首次启动写入 service.json 的 user_id，之后页面与打 8090 的请求都带这个 X-User-Id。

func newClientUserID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("u%x", os.Getpid())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func clientUserID() string {
	serviceMu.Lock()
	defer serviceMu.Unlock()
	if serviceUserID == "" {
		serviceUserID = newClientUserID()
	}
	return serviceUserID
}

type serviceDisk struct {
	StockBase string `json:"stock_base,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

func readServiceDisk(path string) serviceDisk {
	var saved serviceDisk
	if path == "" {
		return saved
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return saved
	}
	_ = json.Unmarshal(raw, &saved)
	return saved
}

func writeServiceDisk(path string, saved serviceDisk) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, _ := json.Marshal(saved)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ensureClientUserID 读盘拿已有 user_id；没有就生成并写回（与 stock_base 共存）。
func ensureClientUserID(path string) string {
	serviceMu.Lock()
	defer serviceMu.Unlock()
	if uid := strings.TrimSpace(serviceUserID); uid != "" {
		return uid
	}
	saved := readServiceDisk(path)
	uid := strings.TrimSpace(saved.UserID)
	if uid == "" {
		uid = newClientUserID()
		saved.UserID = uid
		_ = writeServiceDisk(path, saved)
	}
	serviceUserID = uid
	return uid
}
