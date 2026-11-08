package server

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
)

// hiddenStocks are the stocks the user stopped looking at in the tracking list. Hiding only changes
// what this page shows: the predictions stay on file and still count in the hit rate.
type hiddenStocks struct {
	mu    sync.Mutex
	path  string
	codes map[string]bool
}

func openHidden(path string) *hiddenStocks {
	h := &hiddenStocks{path: path, codes: map[string]bool{}}
	if path == "" {
		return h
	}
	var file struct {
		Codes []string `json:"codes"`
	}
	if raw, err := os.ReadFile(path); err == nil && json.Unmarshal(raw, &file) == nil {
		for _, c := range file.Codes {
			h.codes[c] = true
		}
	}
	return h
}

func (h *hiddenStocks) Has(code string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.codes[code]
}

// Set hides or shows one stock; an empty code with on=false shows them all again.
func (h *hiddenStocks) Set(code string, on bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case code == "" && !on:
		h.codes = map[string]bool{}
	case code == "":
		return
	case on:
		h.codes[code] = true
	default:
		delete(h.codes, code)
	}
	h.save()
}

func (h *hiddenStocks) List() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.codes))
	for c := range h.codes {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func (h *hiddenStocks) save() {
	if h.path == "" {
		return
	}
	codes := make([]string, 0, len(h.codes))
	for c := range h.codes {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	raw, err := json.Marshal(map[string]any{"codes": codes})
	if err != nil {
		return
	}
	tmp := h.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, h.path)
	}
}
