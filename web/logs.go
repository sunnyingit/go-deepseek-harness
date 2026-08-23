package main

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultLogTailBytes = 256 * 1024

type logView struct {
	Path      string `json:"path"`
	Available bool   `json:"available"`
	Hint      string `json:"hint,omitempty"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
	Mtime     string `json:"mtime,omitempty"`
	Text      string `json:"text"`
}

func (s *Server) handleGetLogs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.runtimeLogView(defaultLogTailBytes))
}

func (s *Server) runtimeLogView(maxBytes int64) logView {
	path := s.runtimeLogPath()
	out := logView{Path: path}
	if path == "" {
		out.Hint = "当前进程没有关联运行日志文件。桌面客户端会写入 web.log；用浏览器直接跑服务时请看终端。"
		return out
	}
	text, size, truncated, mtime, err := tailFile(path, maxBytes)
	if err != nil {
		if os.IsNotExist(err) {
			out.Hint = "还没有日志文件。启动过桌面客户端后会出现在这里。"
			return out
		}
		out.Hint = "读取日志失败：" + err.Error()
		return out
	}
	out.Available = true
	out.Size = size
	out.Truncated = truncated
	if !mtime.IsZero() {
		out.Mtime = mtime.Format(time.RFC3339)
	}
	out.Text = text
	return out
}

// runtimeLogPath is the file Electron tees backend stdout/stderr into.
// WEB_DEMO_LOG_FILE wins; otherwise a web.log sitting next to data_dir
// (userData/web.log when data_dir is userData/data).
func (s *Server) runtimeLogPath() string {
	if v := strings.TrimSpace(os.Getenv("WEB_DEMO_LOG_FILE")); v != "" {
		return filepath.Clean(v)
	}
	s.cfgMu.Lock()
	dataDir := strings.TrimSpace(s.cfg.DataDir)
	s.cfgMu.Unlock()
	if dataDir == "" {
		return ""
	}
	sibling := filepath.Join(filepath.Dir(dataDir), "web.log")
	if st, err := os.Stat(sibling); err == nil && !st.IsDir() {
		return sibling
	}
	return filepath.Join(dataDir, "web.log")
}

func tailFile(path string, maxBytes int64) (text string, size int64, truncated bool, mtime time.Time, err error) {
	if maxBytes <= 0 {
		maxBytes = defaultLogTailBytes
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, false, time.Time{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", 0, false, time.Time{}, err
	}
	size = st.Size()
	mtime = st.ModTime()
	start := int64(0)
	if size > maxBytes {
		start = size - maxBytes
		truncated = true
	}
	buf := make([]byte, size-start)
	if len(buf) > 0 {
		if _, err := f.ReadAt(buf, start); err != nil {
			return "", size, truncated, mtime, err
		}
	}
	if truncated {
		if i := bytes.IndexByte(buf, '\n'); i >= 0 && i+1 < len(buf) {
			buf = buf[i+1:]
		}
	}
	return strings.ToValidUTF8(string(buf), "\uFFFD"), size, truncated, mtime, nil
}
