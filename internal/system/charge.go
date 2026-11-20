// SPDX-License-Identifier: Apache-2.0

package system

import "time"

// FullCharge is the limit value that means "no limit".
const FullCharge = 100

// LimitPresets are the quick-pick charge limits the drawer offers.
var LimitPresets = []int{60, 80, 90}

// DefaultLimit is used when no preferred limit has been saved yet.
const DefaultLimit = 80

// LimitActive reports whether a daemon-reported limit is actually limiting.
// 0 means the daemon did not report one.
func LimitActive(limit int) bool { return limit > 0 && limit < FullCharge }

// PreferredLimit returns the limit to restore when the switch is turned back
// on: the saved preference when it is a real limit, otherwise DefaultLimit.
func PreferredLimit(saved int) int {
	if LimitActive(saved) {
		return saved
	}
	return DefaultLimit
}

// ShouldRestore decides whether a pending one-off full charge is over: the user
// turned the limit off with "re-enable after unplugging" set, and the charger is
// now disconnected. acKnown false (no AC supply found) never restores, so a
// machine whose adapter cannot be read keeps the user's explicit choice.
func ShouldRestore(pending, acKnown, acOnline bool) bool {
	return pending && acKnown && !acOnline
}

// ReprobeDue decides whether to look again for desktop integrations that were
// missing: only while some are missing, and at most once per every.
func ReprobeDue(complete bool, last, now time.Time, every time.Duration) bool {
	if complete {
		return false
	}
	return last.IsZero() || now.Sub(last) >= every
}
