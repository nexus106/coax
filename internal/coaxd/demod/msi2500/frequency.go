// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package msi2500

// frequencyPermutation maps each pre-interleaver carrier to its received index.
// STD-B31 3.11.2: transpose segments, rotate left by segment number, then
// assign the rotated carrier to the destination given in Table 3-13.
func frequencyPermutation(mode int, partial bool) []int {
	n := 96 << (mode - 1)
	out := make([]int, 13*n)
	first := 0
	if partial {
		for i := 0; i < n; i++ {
			out[i] = carrierRandomizing[mode-1][i]
		}
		first = 1
	}
	for j := 0; j < (13-first)*n; j++ {
		seg := j%(13-first) + first
		carrier := j / (13 - first)
		rot := (carrier - seg + n) % n
		out[first*n+j] = seg*n + carrierRandomizing[mode-1][rot]
	}
	return out
}
