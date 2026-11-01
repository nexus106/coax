// Copyright 2013 Miroslav Slugen; 2025 Peter Hackenberg; 2026 Till0196
// SPDX-License-Identifier: GPL-2.0-or-later
// PLL and gain programming adapted from libmirisdr-5 soft.c and gain.c.

// Package msi001 drives the MSi001 UHF tuner through a serial register bus.
package msi001

import "errors"

// Bus writes one complete tuner configuration as an ordered SPI transaction.
// The MSi2500 bridge implements this over its USB control endpoint.
type Bus interface{ WriteRegisters([]uint32) error }

// Tuner is an MSi001 reached through its bridge-provided register bus.
type Tuner struct{ bus Bus }

func New(bus Bus) *Tuner { return &Tuner{bus: bus} }

// Tune selects UHF frequencies [330,960) MHz and manual gain 0..85 dB.
// UHF gain comprises the 7 dB LNA, 19 dB mixer and 0..59 dB baseband stage.
func (tuner *Tuner) Tune(f uint32, gain int) error {
	if f < 330000000 || f >= 960000000 {
		return errors.New("msi001: frequency outside supported UHF band")
	}
	if gain < 0 || gain > 85 {
		return errors.New("msi001: UHF gain must be 0..85 dB")
	}
	const div = uint64(4)
	fvco := uint64(f) * div
	n := fvco / 96000000
	thresh := uint64(96000000) / div
	frac := (fvco % 96000000) / div
	a, c := thresh, frac
	for c != 0 {
		a, c = c, a%c
	}
	thresh /= a
	frac /= a
	scale := (thresh + 4094) / 4095
	thresh = (thresh + scale/2) / scale
	frac = (frac + scale/2) / scale
	actual := 96000000 * (n*thresh + frac) / (thresh * div)
	if uint64(f) < actual {
		frac--
	}
	actual = 96000000 * (n*thresh + frac) / (thresh * div)
	afc := (uint64(f) - actual) * thresh * 4096 * div / 96000000
	lna, mixer, bb := uint32(1), uint32(1), gain
	if gain >= 26 {
		lna = 0
		mixer = 0
		bb -= 26
	} else if gain >= 19 {
		mixer = 0
		bb -= 19
	}
	words := []uint32{0x0e, 3 | uint32(afc&4095)<<4, 8<<4 | 1<<10 | 3<<12 | 7<<14 | 2<<17, 5 | uint32(thresh)<<4 | 0x28<<16, 2 | uint32(frac&4095)<<4 | uint32(n&63)<<16, 0x0d, 1 | uint32(59-bb)<<4 | mixer<<12 | lna<<13 | 2<<14, 6 | 0x1f<<4 | 0x800<<10}
	return tuner.bus.WriteRegisters(words)
}
