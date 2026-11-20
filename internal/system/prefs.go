// SPDX-License-Identifier: Apache-2.0

package system

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dahui/z13gui/internal/theme"
)

// Prefs are the drawer's own settings that are not theme choices. They live in
// a separate file from config.toml so the theme code, which rewrites that file
// whole, cannot drop them.
type Prefs struct {
	// ChargeLimit is the limit to restore when the charge-limit switch is turned
	// back on (the daemon only knows the current value, which is 100 while off).
	ChargeLimit int
	// RestoreOnUnplug re-enables the limit once the charger is removed after a
	// one-off full charge.
	RestoreOnUnplug bool
	// PendingRestore is set while a one-off full charge is in progress. It is
	// persisted so a restart mid-charge still restores the limit.
	PendingRestore bool
	// AutoRotate is the auto-rotate policy applied when rotation is unlocked
	// ("in-tablet-mode" or "always").
	AutoRotate string
	// Tab is the drawer tab shown last, reopened on the next start.
	Tab string
}

// DefaultPrefs is what a missing or unreadable file yields.
func DefaultPrefs() Prefs {
	return Prefs{
		ChargeLimit:     DefaultLimit,
		RestoreOnUnplug: true,
		AutoRotate:      "in-tablet-mode",
		Tab:             "power",
	}
}

// PrefsPath is ~/.config/z13gui/prefs.conf (XDG_CONFIG_HOME respected).
func PrefsPath() string {
	return filepath.Join(theme.XDGConfigHome(), "z13gui", "prefs.conf")
}

// ParsePrefs reads key = value lines; unknown keys and bad values keep defaults.
func ParsePrefs(data string) Prefs {
	p := DefaultPrefs()
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "charge_limit":
			if n, err := strconv.Atoi(v); err == nil && LimitActive(n) {
				p.ChargeLimit = n
			}
		case "restore_on_unplug":
			if b, err := strconv.ParseBool(v); err == nil {
				p.RestoreOnUnplug = b
			}
		case "pending_restore":
			if b, err := strconv.ParseBool(v); err == nil {
				p.PendingRestore = b
			}
		case "auto_rotate":
			if v == "always" || v == "in-tablet-mode" {
				p.AutoRotate = v
			}
		case "tab":
			if v == "power" || v == "lighting" || v == "display" {
				p.Tab = v
			}
		}
	}
	return p
}

// Format renders prefs in the file format ParsePrefs reads.
func (p Prefs) Format() string {
	return fmt.Sprintf("# z13gui drawer preferences\n"+
		"charge_limit = %d\nrestore_on_unplug = %t\npending_restore = %t\n"+
		"auto_rotate = \"%s\"\ntab = \"%s\"\n",
		p.ChargeLimit, p.RestoreOnUnplug, p.PendingRestore, p.AutoRotate, p.Tab)
}

// LoadPrefs reads PrefsPath, returning defaults when it does not exist.
func LoadPrefs() Prefs {
	data, err := os.ReadFile(PrefsPath())
	if err != nil {
		return DefaultPrefs()
	}
	return ParsePrefs(string(data))
}

// SavePrefs writes p to PrefsPath. Failures are logged, not returned: a lost
// preference is not worth interrupting the user over.
func SavePrefs(p Prefs) {
	path := PrefsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		slog.Warn("failed to create config dir", "path", path, "err", err)
		return
	}
	if err := os.WriteFile(path, []byte(p.Format()), 0o644); err != nil {
		slog.Warn("failed to write prefs", "path", path, "err", err)
	}
}
