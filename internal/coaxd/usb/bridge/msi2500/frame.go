// Copyright 2026 Till0196
// SPDX-License-Identifier: GPL-2.0-or-later
// 336 wire layout based on libmirisdr-5 (Miroslav Slugen, 2013).
package msi2500

import (
	"encoding/binary"
	"errors"
)

// decoder retains at most one USB transfer plus a partial 1024-byte frame.
// A frame has a 16-byte header followed by 336 little-endian packed I/Q pairs.
type decoder struct {
	raw     []byte
	next    uint32
	seen    bool
	samples [336]complex64
	pending []complex64
	loss    bool
}

func (d *decoder) push(b []byte) error {
	if len(b) > 65536 || len(d.raw)+len(b) > 65536+1023 {
		return errors.New("msi2500: IQ transfer exceeds bounded buffer")
	}
	d.raw = append(d.raw, b...)
	return nil
}

// read drains only the complete frames already buffered by the last transfer.
// A gap ends the current batch before its first sample, preserving the meaning
// of loss for callers that reset their demodulator at dst[0].
func (d *decoder) read(dst []complex64) (n int, loss bool) {
	for n < len(dst) {
		if len(d.pending) == 0 {
			if len(d.raw) < 1024 {
				break
			}
			b := d.raw[:1024]
			counter := binary.LittleEndian.Uint32(b)
			gap := d.seen && counter != d.next
			if gap && n > 0 {
				break
			}
			d.loss = gap
			d.seen = true
			d.next = counter + 336
			for i := range d.samples {
				j := 16 + 3*i
				iv := int16(uint16(b[j])<<4 | uint16(b[j+1]&15)<<12)
				qv := int16(uint16(b[j+1]&240) | uint16(b[j+2])<<8)
				d.samples[i] = complex(float32(iv)/32768, float32(qv)/32768)
			}
			d.raw = d.raw[1024:]
			d.pending = d.samples[:]
		}
		if n == 0 {
			loss = d.loss
		}
		moved := copy(dst[n:], d.pending)
		d.pending = d.pending[moved:]
		d.loss = false
		n += moved
	}
	return n, loss
}
