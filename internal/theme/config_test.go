// Copyright 2026 Jeff Hagadorn
// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppConfig_Default(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	cfg := LoadAppConfig()
	if cfg.Theme != "rog-dark" {
		t.Errorf("Theme = %q, want rog-dark", cfg.Theme)
	}
	if cfg.Accent != "" {
		t.Errorf("Accent = %q, want empty", cfg.Accent)
	}
}

func TestLoadAppConfig_WithTheme(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "z13gui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("theme = \"nord\"\naccent = \"blue\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadAppConfig()
	if cfg.Theme != "nord" {
		t.Errorf("Theme = %q, want nord", cfg.Theme)
	}
	if cfg.Accent != "blue" {
		t.Errorf("Accent = %q, want blue", cfg.Accent)
	}
}

func TestLoadAppConfig_Comments(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "z13gui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := "# config file\ntheme = \"gruvbox-dark\" # nice theme\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadAppConfig()
	if cfg.Theme != "gruvbox-dark" {
		t.Errorf("Theme = %q, want gruvbox-dark", cfg.Theme)
	}
}

func TestLoadAppConfig_EmptyThemeKeepsDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "z13gui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("theme = \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := LoadAppConfig()
	if cfg.Theme != "rog-dark" {
		t.Errorf("Theme = %q, want rog-dark (default for empty value)", cfg.Theme)
	}
}

func TestSaveAppConfig_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	original := AppConfig{Theme: "catppuccin-mocha", Accent: "sapphire"}
	SaveAppConfig(original)

	loaded := LoadAppConfig()
	if loaded.Theme != original.Theme {
		t.Errorf("Theme = %q, want %q", loaded.Theme, original.Theme)
	}
	if loaded.Accent != original.Accent {
		t.Errorf("Accent = %q, want %q", loaded.Accent, original.Accent)
	}
}

func TestSaveAppConfig_NoAccent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	SaveAppConfig(AppConfig{Theme: "nord"})
	loaded := LoadAppConfig()
	if loaded.Theme != "nord" {
		t.Errorf("Theme = %q, want nord", loaded.Theme)
	}
	if loaded.Accent != "" {
		t.Errorf("Accent = %q, want empty", loaded.Accent)
	}
}

func TestSaveAppConfig_CreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	SaveAppConfig(AppConfig{Theme: "nord"})
	dir := filepath.Join(tmp, "z13gui")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("config dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("config path is not a directory")
	}
}

func TestXDGConfigHome_EnvSet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got := XDGConfigHome()
	if got != "/custom/config" {
		t.Errorf("XDGConfigHome() = %q, want /custom/config", got)
	}
}

func TestXDGConfigHome_Fallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got := XDGConfigHome()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config")
	if got != want {
		t.Errorf("XDGConfigHome() = %q, want %q", got, want)
	}
}

func TestAppConfig_FollowSystemRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := AppConfig{Theme: "rog-dark", FollowSystem: true,
		LightTheme: "one-light", LightAccent: "green", DarkTheme: "one-dark", DarkAccent: "purple"}
	SaveAppConfig(want)
	if got := LoadAppConfig(); got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestAppConfig_Effective(t *testing.T) {
	fixed := AppConfig{Theme: "nord", Accent: "x"}
	if th, ac := fixed.Effective(false); th != "nord" || ac != "x" {
		t.Errorf("not following: %s/%s", th, ac)
	}
	f := AppConfig{Theme: "nord", FollowSystem: true}
	if th, _ := f.Effective(true); th != DefaultDarkTheme {
		t.Errorf("empty dark slot = %s, want %s", th, DefaultDarkTheme)
	}
	if th, _ := f.Effective(false); th != DefaultLightTheme {
		t.Errorf("empty light slot = %s, want %s", th, DefaultLightTheme)
	}
	f.DarkTheme, f.DarkAccent = "tokyo-night", ""
	if th, _ := f.Effective(true); th != "tokyo-night" {
		t.Errorf("dark slot = %s", th)
	}
}

func TestAppConfig_SetSlot(t *testing.T) {
	c := AppConfig{Theme: "nord"}
	c.SetSlot(true, "one-dark", "red")
	if c.Theme != "one-dark" || c.Accent != "red" || c.DarkTheme != "" {
		t.Errorf("fixed mode should set Theme: %+v", c)
	}
	c = AppConfig{Theme: "nord", FollowSystem: true}
	c.SetSlot(true, "one-dark", "red")
	c.SetSlot(false, "one-light", "")
	if c.Theme != "nord" || c.DarkTheme != "one-dark" || c.DarkAccent != "red" || c.LightTheme != "one-light" {
		t.Errorf("follow mode should fill the slots and keep Theme: %+v", c)
	}
}
