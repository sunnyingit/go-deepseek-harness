package filesystem

import (
	"time"

	"go-harness/skill/skill"
)

// Register builds a Provider from config and registers it with registry,
// wiring skill.RegisterProvider's Control plumbing so the caller doesn't
// have to.
//
// When pollInterval is positive, a background goroutine calls
// control.Invalidate() every pollInterval until the provider is
// unregistered — a deliberately simple stand-in for the TS original's real
// Chokidar-based instant file-change invalidation (watched roots, batched
// events, symlink handling — see README.md). This just clears
// skill.Registry's Get cache on a timer so a disk change eventually
// becomes visible without the caller having to call Invalidate themselves;
// it does not know or care whether anything actually changed. Zero (the
// default) disables polling entirely: List/Snapshot still always scan disk
// fresh, only Get's candidate-resolution cache can go stale between calls.
func Register(registry *skill.Registry, name string, config Config, pollInterval time.Duration) (func(), error) {
	return registry.RegisterProvider(func(control skill.Control) (skill.Provider, error) {
		if pollInterval > 0 {
			go pollInvalidate(control, pollInterval)
		}
		return New(name, config), nil
	})
}

func pollInvalidate(control skill.Control, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-control.Ctx.Done():
			return
		case <-ticker.C:
			control.Invalidate()
		}
	}
}
