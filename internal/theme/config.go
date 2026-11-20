// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// AppConfig holds app-level preferences persisted to config.toml.
type AppConfig struct {
	Theme  string // built-in theme ID; empty = use default
	Accent string // accent ID within the theme; "" = use theme default

	// FollowSystem picks the theme from the desktop's light/dark preference
	// instead of Theme: LightTheme/LightAccent when the system is light,
	// DarkTheme/DarkAccent when it is dark. Theme/Accent stay saved, so turning
	// it off returns to the fixed choice.
	FollowSystem bool
	LightTheme   string
	LightAccent  string
	DarkTheme    string
	DarkAccent   string
}

// Default slot themes for FollowSystem: the One pair, light and dark.
const (
	DefaultLightTheme = "one-light"
	DefaultDarkTheme  = "one-dark"
)

// Effective returns the theme and accent to show, given whether the system is
// in dark mode. Empty slots fall back to the One pair.
func (c AppConfig) Effective(systemDark bool) (themeID, accentID string) {
	if !c.FollowSystem {
		return c.Theme, c.Accent
	}
	if systemDark {
		if c.DarkTheme == "" {
			return DefaultDarkTheme, ""
		}
		return c.DarkTheme, c.DarkAccent
	}
	if c.LightTheme == "" {
		return DefaultLightTheme, ""
	}
	return c.LightTheme, c.LightAccent
}

// SetSlot stores a theme choice where it belongs: the dark or light slot when
// following the system, otherwise the fixed Theme/Accent.
func (c *AppConfig) SetSlot(dark bool, themeID, accentID string) {
	switch {
	case !c.FollowSystem:
		c.Theme, c.Accent = themeID, accentID
	case dark:
		c.DarkTheme, c.DarkAccent = themeID, accentID
	default:
		c.LightTheme, c.LightAccent = themeID, accentID
	}
}

// LoadAppConfig reads ~/.config/z13gui/config.toml.
// Returns a default config (theme "rog-dark") if the file doesn't exist or can't be parsed.
func LoadAppConfig() AppConfig {
	path := filepath.Join(XDGConfigHome(), "z13gui", "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{Theme: "rog-dark"}
	}
	cfg := AppConfig{Theme: "rog-dark"}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, " #"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "theme":
			if v != "" {
				cfg.Theme = v
			}
		case "accent":
			cfg.Accent = v
		case "follow_system":
			cfg.FollowSystem = v == "true"
		case "light_theme":
			cfg.LightTheme = v
		case "light_accent":
			cfg.LightAccent = v
		case "dark_theme":
			cfg.DarkTheme = v
		case "dark_accent":
			cfg.DarkAccent = v
		}
	}
	return cfg
}

// SaveAppConfig writes the app config to ~/.config/z13gui/config.toml.
func SaveAppConfig(cfg AppConfig) {
	dir := filepath.Join(XDGConfigHome(), "z13gui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("failed to create config dir", "path", dir, "err", err)
		return
	}
	content := "# z13gui app configuration\ntheme = \"" + cfg.Theme + "\"\n"
	if cfg.Accent != "" {
		content += "accent = \"" + cfg.Accent + "\"\n"
	}
	if cfg.FollowSystem {
		content += "follow_system = true\n"
	}
	for _, kv := range [][2]string{
		{"light_theme", cfg.LightTheme}, {"light_accent", cfg.LightAccent},
		{"dark_theme", cfg.DarkTheme}, {"dark_accent", cfg.DarkAccent},
	} {
		if kv[1] != "" {
			content += kv[0] + " = \"" + kv[1] + "\"\n"
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		slog.Warn("failed to write config", "err", err)
	}
}

// XDGConfigHome returns $XDG_CONFIG_HOME or falls back to ~/.config.
func XDGConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("failed to get home directory", "err", err)
		return "/tmp/.config"
	}
	return filepath.Join(home, ".config")
}
