package server

import (
	"fmt"
	"os"
	"path/filepath"
)

// exposeBundledNodeModules prepends the desktop runtime's node_modules to
// NODE_PATH so `go run ./cmd/web` can require pptxgenjs / sharp / react-icons
// the same way the packaged Electron app does.
func exposeBundledNodeModules() {
	var cands []string
	if v := os.Getenv("HARNESS_NODE_MODULES"); v != "" {
		cands = append(cands, v)
	}
	if wd, err := os.Getwd(); err == nil {
		cands = append(cands,
			filepath.Join(wd, "native", "runtime", "node-packages", "node_modules"),
			filepath.Join(wd, "..", "native", "runtime", "node-packages", "node_modules"),
		)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		cands = append(cands,
			filepath.Join(dir, "..", "node-packages", "node_modules"),
			filepath.Join(dir, "..", "runtime", "node-packages", "node_modules"),
		)
	}
	seen := map[string]bool{}
	var found []string
	for _, c := range cands {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		st, err := os.Stat(abs)
		if err != nil || !st.IsDir() || seen[abs] {
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "pptxgenjs")); err != nil {
			continue
		}
		seen[abs] = true
		found = append(found, abs)
	}
	if len(found) == 0 {
		return
	}
	sep := string(os.PathListSeparator)
	cur := os.Getenv("NODE_PATH")
	if cur != "" {
		found = append(found, cur)
	}
	os.Setenv("NODE_PATH", joinPathList(found, sep))
	fmt.Println("NODE_PATH includes bundled pptxgenjs at", found[0])
}

func joinPathList(parts []string, sep string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		if p != "" {
			out += sep + p
		}
	}
	return out
}
