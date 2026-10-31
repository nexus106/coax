// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package isdbt

import (
	"fmt"
	"math"
	"math/bits"
)

// fft is an in-place radix-2 transform. Plans are immutable and reusable.
type fft struct {
	n       int
	reverse []int
	roots   []complex64
}

func newFFT(n int) (*fft, error) {
	if n < 2 || n&(n-1) != 0 {
		return nil, fmt.Errorf("FFT length must be a power of two: %d", n)
	}
	f := &fft{n: n, reverse: make([]int, n), roots: make([]complex64, n/2)}
	shift := bits.UintSize - bits.Len(uint(n)) + 1
	for i := range f.reverse {
		f.reverse[i] = int(bits.Reverse(uint(i)) >> shift)
	}
	for k := range f.roots {
		a := -2 * math.Pi * float64(k) / float64(n)
		s, c := math.Sincos(a)
		f.roots[k] = complex(float32(c), float32(s))
	}
	return f, nil
}
func (f *fft) transform(x []complex64) {
	for i, j := range f.reverse {
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for width := 2; width <= f.n; width *= 2 {
		half := width / 2
		stride := f.n / width
		for start := 0; start < f.n; start += width {
			for k := 0; k < half; k++ {
				a := x[start+k]
				b := x[start+k+half] * f.roots[k*stride]
				x[start+k] = a + b
				x[start+k+half] = a - b
			}
		}
	}
}
