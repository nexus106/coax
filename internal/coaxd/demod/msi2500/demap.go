// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package msi2500

import "math"

func modulationBits(q int) int {
	switch q {
	case 4:
		return 2
	case 16:
		return 4
	case 64:
		return 6
	}
	return 0
}

// demap uses the axis Gray labels of STD-B31 Figs 3-15,3-17,3-19.
func demap(dst []byte, src []complex64, q int) []byte {
	b := modulationBits(q)
	offset := len(dst)
	needed := offset + len(src)*b
	if needed > cap(dst) {
		grown := make([]byte, needed)
		copy(grown, dst)
		dst = grown
	} else {
		dst = dst[:needed]
	}
	scale := float32(math.Sqrt(float64(q-1) * 2 / 3))
	for index, c := range src {
		x, y := real(c)*scale, imag(c)*scale
		var v [6]byte
		if x < 0 {
			v[0] = 1
			x = -x
		}
		if y < 0 {
			v[1] = 1
			y = -y
		}
		if b >= 4 {
			if q == 16 {
				if x < 2 {
					v[2] = 1
				}
				if y < 2 {
					v[3] = 1
				}
			} else {
				if x < 4 {
					v[2] = 1
				}
				if y < 4 {
					v[3] = 1
				}
				if x > 2 && x < 6 {
					v[4] = 1
				}
				if y > 2 && y < 6 {
					v[5] = 1
				}
			}
		}
		base := offset + index*b
		copy(dst[base:base+b], v[:b])
	}
	return dst
}
