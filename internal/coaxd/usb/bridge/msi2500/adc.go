// Copyright 2013 Miroslav Slugen; 2025 Peter Hackenberg; 2026 Till0196
// SPDX-License-Identifier: GPL-2.0-or-later
// Register programming adapted from libmirisdr-5 reg.c, adc.c, hard.c,
// soft.c and gain.c, distributed under GPL-2.0-or-later. No GPL-3-only source.

package msi2500

func (b *Bridge) configureADCClock() error {
	// 48 MHz reference, ADC VCO >=202 MHz, 21-bit fractional PLL.
	const divider = 4
	const vco = uint64(SampleRate) * divider * 12
	const fraction = (vco % 48000000) * 0x200000 / 48000000
	const reg3 = 3 | (divider/2-1)<<2 | ((fraction>>20)&1)<<7 | ((vco/48000000)&15)<<8 | 5<<12 | 1<<16
	for _, v := range [][2]uint32{{7, 0x85}, {4, uint32(fraction & 0xfffff)}, {3, uint32(reg3)}} {
		if err := b.reg(byte(v[0]), v[1]); err != nil {
			return err
		}
	}
	return nil
}
