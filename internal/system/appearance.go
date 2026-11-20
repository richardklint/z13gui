// SPDX-License-Identifier: Apache-2.0

package system

import (
	"strconv"
	"strings"
)

// Colour-scheme values of the XDG desktop portal's
// org.freedesktop.appearance color-scheme setting.
const (
	SchemeNoPreference = 0
	SchemeDark         = 1
	SchemeLight        = 2
)

// ParseColorScheme reads the portal's ReadOne reply as busctl prints it
// ("v u 1"). ok is false when the reply is not a uint32 scheme.
func ParseColorScheme(busctl string) (scheme uint32, ok bool) {
	f := strings.Fields(busctl)
	if len(f) < 2 || f[len(f)-2] != "u" {
		return 0, false
	}
	n, err := strconv.ParseUint(f[len(f)-1], 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

// SchemeIsDark maps a scheme to dark/light. "No preference" counts as dark:
// it is what a desktop without a light/dark setting reports, and the drawer's
// default themes are dark.
func SchemeIsDark(scheme uint32) bool { return scheme != SchemeLight }
