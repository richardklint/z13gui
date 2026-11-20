// SPDX-License-Identifier: Apache-2.0

package system

import "math"

// SnapTo rounds v to the nearest multiple of step and clamps it to [lo, hi].
// The brightness slider snaps through it, so a drag lands on 5% marks.
func SnapTo(v, step, lo, hi float64) float64 {
	if step > 0 {
		v = math.Round(v/step) * step
	}
	return math.Max(lo, math.Min(hi, v))
}
