// SPDX-License-Identifier: Apache-2.0

package gui

// icons.go — the drawer's own symbolic icons.
//
// The tab bar and header need icons that neither Breeze nor every gamescope
// session ships (power-profile-*, video-display-*), and a missing icon renders
// as nothing at all. They are written to the user cache dir at startup and added
// to the icon theme's search path, so GTK treats them as ordinary symbolic icons
// and recolours them with the theme's foreground.
//
// Every shape is a FILLED path. GTK recolours symbolic SVGs by injecting
// `fill: <colour> !important`, so a stroked outline with fill="none" would come
// out as a solid blob.

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

const svgHead = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16"><path fill="#bebebe" fill-rule="evenodd" d="`
const svgTail = `"/></svg>`

var drawerIcons = map[string]string{
	// Lightning bolt.
	"z13-power-symbolic": "M9.5 1 3 9h4.2l-1 6L13 6.8H8.6z",
	// Sun: disc plus eight rays.
	"z13-lighting-symbolic": "M8 5a3 3 0 1 0 0 6 3 3 0 1 0 0-6z" +
		"M7.25 0h1.5v3h-1.5zM7.25 13h1.5v3h-1.5zM0 7.25h3v1.5H0zM13 7.25h3v1.5h-3z" +
		"M2.1 3.2l1.1-1.1 2.1 2.1-1.1 1.1zM10.7 11.8l1.1-1.1 2.1 2.1-1.1 1.1z" +
		"M2.1 12.8l2.1-2.1 1.1 1.1-2.1 2.1zM10.7 4.2l2.1-2.1 1.1 1.1-2.1 2.1z",
	// Monitor: frame (even-odd hole) and stand.
	"z13-display-symbolic": "M1 2h14v9.5H1zM2.5 3.5v6.5h11V3.5z" +
		"M7.25 11h1.5v2.5h-1.5zM4.5 13.5h7V15h-7z",
	// Keyboard: frame, two rows of keys, space bar.
	"z13-keyboard-symbolic": "M0.5 3.5h15v9h-15zM2 5v6h12V5z" +
		"M3 6h1.5v1.5H3zM5.5 6H7v1.5H5.5zM8 6h1.5v1.5H8zM10.5 6H12v1.5h-1.5z" +
		"M4.5 8.5h7V10h-7z",
	// Cross.
	"z13-close-symbolic": "M3.4 2.3 8 6.9l4.6-4.6 1.1 1.1L9.1 8l4.6 4.6-1.1 1.1L8 9.1l-4.6 4.6-1.1-1.1L6.9 8 2.3 3.4z",
	// Chevron pointing right.
	"z13-chevron-symbolic": "M5.3 2.3 11 8l-5.7 5.7-1.1-1.1L8.8 8 4.2 3.4z",
}

// installIcons writes the icons and registers their directory with the
// display's icon theme. A failure is logged and leaves the icons blank — the
// buttons still carry text or tooltips, so nothing becomes unusable.
func installIcons() {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "z13gui", "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("cannot create icon dir", "path", dir, "err", err)
		return
	}
	for name, path := range drawerIcons {
		file := filepath.Join(dir, name+".svg")
		content := svgHead + path + svgTail
		if old, err := os.ReadFile(file); err == nil && string(old) == content {
			continue
		}
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			slog.Warn("cannot write icon", "path", file, "err", err)
		}
	}
	gtk.IconThemeGetForDisplay(gdk.DisplayGetDefault()).AddSearchPath(dir)
}
