// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package msi2500

// STD-B31 Fig3-23 uses delay I*((5*i)mod96); complementary delays
// recover all carriers at the same time with maximum delay 95*I.
type timeDeinterleaver struct {
	memory [][]complex64
	at     []int
}

func newTimeDeinterleaver(n, depth int) *timeDeinterleaver {
	d := &timeDeinterleaver{memory: make([][]complex64, n), at: make([]int, n)}
	for i := range d.memory {
		d.memory[i] = make([]complex64, depth*(95-(i*5)%96))
	}
	return d
}
func (d *timeDeinterleaver) push(dst, src []complex64) {
	for i, v := range src {
		m := d.memory[i]
		if len(m) > 0 {
			p := d.at[i]
			dst[i] = m[p]
			m[p] = v
			d.at[i] = (p + 1) % len(m)
		} else {
			dst[i] = v
		}
	}
}

// The complementary delays reverse Figs3-14,3-16,3-18; maximum120
// carrier symbols applies to QPSK,16QAM,and64QAM alike.
type bitDeinterleaver struct {
	memory [][]byte
	at     []int
}

func newBitDeinterleaver(bits int) *bitDeinterleaver {
	d := &bitDeinterleaver{memory: make([][]byte, bits), at: make([]int, bits)}
	for i := range d.memory {
		d.memory[i] = make([]byte, 120-i*120/(bits-1))
	}
	return d
}
func (d *bitDeinterleaver) push(dst, src []byte) []byte {
	for i, v := range src {
		b := i % len(d.memory)
		m := d.memory[b]
		if len(m) > 0 {
			p := d.at[b]
			old := m[p]
			m[p] = v
			v = old
			d.at[b] = (p + 1) % len(m)
		}
		dst = append(dst, v)
	}
	return dst
}
