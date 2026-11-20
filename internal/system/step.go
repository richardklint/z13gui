// SPDX-License-Identifier: Apache-2.0

package system

import "math"

// SnapStep returns the value a −/+ tap moves v to: the next multiple of step
// in direction dir (+1 or −1), skipping a multiple closer than minMove so a
// tap always makes a visible change (9 → 15 rather than 10, but 7 → 10).
// The result is clamped to [lo, hi].
func SnapStep(v float64, step, minMove float64, dir int, lo, hi float64) float64 {
	if step <= 0 || dir == 0 {
		return v
	}
	v = math.Round(v)
	var next float64
	if dir > 0 {
		next = (math.Floor(v/step) + 1) * step
		if next-v < minMove {
			next += step
		}
	} else {
		next = (math.Ceil(v/step) - 1) * step
		if v-next < minMove {
			next -= step
		}
	}
	return math.Max(lo, math.Min(hi, next))
}
