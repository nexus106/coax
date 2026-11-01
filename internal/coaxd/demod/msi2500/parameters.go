// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

// Package msi2500 implements ISDB-T software demodulation for MSi2500 IQ.
// References: ARIB STD-B31, chapters 3.11 through 3.15. No GNU Radio code is used.
package msi2500

import (
	"errors"
	"fmt"
)

// ErrUnsupported reports a parity-verified transmission the decoder cannot decode.
var ErrUnsupported = errors.New("unsupported ISDB-T transmission")

type LayerParameters struct{ Constellation, Rate, Interleave, Segments int }
type Parameters struct {
	Mode, GuardDenominator int
	PartialReception       bool
	Layers                 [3]LayerParameters
}

// parseTMCC validates the (184,102) cyclic code before using any configuration.
func parseTMCC(frame []byte, mode int) (Parameters, error) {
	p := Parameters{Mode: mode}
	if len(frame) != 204 || mode < 1 || mode > 3 {
		return p, fmt.Errorf("invalid TMCC frame or mode")
	}
	sync := bitNumber(frame[1:17])
	if sync != 0x35ee && sync != 0xca11 {
		return p, fmt.Errorf("TMCC synchronization word is invalid")
	}
	check := append([]byte(nil), frame[20:]...)
	for i := 0; i < 102; i++ {
		if check[i]&1 != 0 {
			for _, power := range []int{82, 77, 76, 71, 67, 66, 56, 52, 48, 40, 36, 34, 24, 22, 18, 10, 4, 0} {
				check[i+82-power] ^= 1
			}
		}
	}
	for _, b := range check[102:] {
		if b != 0 {
			return p, fmt.Errorf("TMCC parity is invalid")
		}
	}
	if bitNumber(frame[20:22]) != 0 {
		return p, fmt.Errorf("%w: TMCC is not terrestrial television", ErrUnsupported)
	}
	p.PartialReception = frame[27] != 0
	total := 0
	for i := range p.Layers {
		s := 28 + 13*i
		mod, rate, inter, segs := bitNumber(frame[s:s+3]), bitNumber(frame[s+3:s+6]), bitNumber(frame[s+6:s+9]), bitNumber(frame[s+9:s+13])
		if mod == 7 && rate == 7 && inter == 7 && segs == 15 {
			continue
		}
		if mod < 1 || mod > 3 || rate > 4 || inter > 3 || segs < 1 || segs > 13 {
			return p, fmt.Errorf("%w: TMCC layer %d parameters", ErrUnsupported, i)
		}
		p.Layers[i] = LayerParameters{[]int{0, 4, 16, 64}[mod], rate, []int{0, 4, 8, 16}[inter] >> (mode - 1), segs}
		total += segs
	}
	if total != 13 {
		return p, fmt.Errorf("TMCC layer segments sum to %d, want 13", total)
	}
	return p, nil
}
func bitNumber(bits []byte) int {
	v := 0
	for _, b := range bits {
		v = v*2 + int(b&1)
	}
	return v
}
