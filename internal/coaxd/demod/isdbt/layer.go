// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package isdbt

import "github.com/Till0196/coax/internal/coaxd/demod/isdbt/fec"

type layerDecoder struct {
	p                                    LayerParameters
	n, frameBytes                        int
	time                                 *timeDeinterleaver
	bit                                  *bitDeinterleaver
	v                                    *fec.Viterbi
	bytes                                *fec.ByteDeinterleaver
	timed                                []complex64
	hard, unbit, decoded, unbyte, packet []byte
	position                             int
	prbs                                 uint16
	packets, failures                    uint64
	consecutiveFailures                  int
}

func newLayerDecoder(p LayerParameters, mode int) *layerDecoder {
	n := p.Segments * (96 << (mode - 1))
	b := modulationBits(p.Constellation)
	v, _ := fec.NewViterbi(p.Rate)
	num := []int{1, 2, 3, 5, 7}[p.Rate]
	den := []int{2, 3, 4, 6, 8}[p.Rate]
	return &layerDecoder{p: p, n: n, frameBytes: 204 * n * b * num / den / 8, time: newTimeDeinterleaver(n, p.Interleave), bit: newBitDeinterleaver(b), v: v, bytes: fec.NewByteDeinterleaver(), timed: make([]complex64, n), prbs: 0x00a9}
}
func (l *layerDecoder) push(dst []byte, src []complex64) []byte {
	l.time.push(l.timed, src)
	l.hard = demap(l.hard[:0], l.timed, l.p.Constellation)
	l.unbit = l.bit.push(l.unbit[:0], l.hard)
	l.decoded = l.v.Decode(l.decoded[:0], l.unbit)
	l.unbyte = l.bytes.Push(l.unbyte[:0], l.decoded)
	for _, b := range l.unbyte {
		if l.position%l.frameBytes == 0 {
			l.prbs = 0x00a9
		}
		mask := nextEnergyByte(&l.prbs)
		if l.position%204 != 203 {
			b ^= mask
		}
		l.packet = append(l.packet, b)
		l.position++
		if len(l.packet) == 204 {
			var word [204]byte
			word[0] = l.packet[203]
			copy(word[1:], l.packet[:203])
			if _, err := fec.DecodeRS(word[:]); err == nil && word[0] == 0x47 {
				dst = append(dst, word[:188]...)
				l.packets++
				l.consecutiveFailures = 0
			} else {
				l.failures++
				l.consecutiveFailures++
			}
			l.packet = l.packet[:0]
		}
	}
	return dst
}

// STD-B31 Fig3-8 numbers D1..D15 from the input end. D1 is bitzero
// here, hence initial 100101010000000 is 0x00a9, not its bit reversal.
func nextEnergyByte(register *uint16) byte {
	var out byte
	for i := 0; i < 8; i++ {
		feedback := ((*register >> 14) ^ (*register >> 13)) & 1
		*register = (*register<<1 | feedback) & 0x7fff
		out = out<<1 | byte(feedback)
	}
	return out
}
