// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package isdbt

import "fmt"

// Config reserves decoder options; the default decodes all transmitted layers.
type Config struct{}

// Decoder recovers 188-byte transport packets from standard-rate ISDB-T IQ.
// Each instance is a single stream; callers serialize PushIQ and status access.
type Decoder struct {
	sync        *Synchronizer
	p           *Parameters
	layers      [3]*layerDecoder
	permutation []int
	ordered     []complex64
	started     bool
	queues      [3][][]byte
	queueAt     [3]int
}

func New(Config) (*Decoder, error)        { return &Decoder{sync: NewSynchronizer()}, nil }
func (d *Decoder) Snapshot() SyncSnapshot { return d.sync.Snapshot() }

// Locked requires verified TMCC and at least five RS-valid packets on every
// active layer. Sixteen consecutive failed words revoke the transport lock.
func (d *Decoder) Locked() bool {
	if d.p == nil {
		return false
	}
	for _, l := range d.layers {
		if l != nil && (l.packets < 5 || l.consecutiveFailures >= 16) {
			return false
		}
	}
	return true
}
func (d *Decoder) PushIQ(samples []complex64, discontinuity bool) ([]byte, error) {
	if discontinuity {
		d.p = nil
		d.started = false
		d.layers = [3]*layerDecoder{}
		d.queues = [3][][]byte{}
		d.queueAt = [3]int{}
	}
	var out []byte
	var err error
	d.sync.PushIQ(samples, discontinuity, func(s Symbol) {
		if s.Parameters != nil && (d.p == nil || *d.p != *s.Parameters) {
			p := *s.Parameters
			d.p = &p
			d.started = false
			d.permutation = frequencyPermutation(p.Mode, p.PartialReception)
			d.ordered = make([]complex64, len(d.permutation))
			// Equalize whole-frame time-interleaver delays before combining layers.
			// Packets retain their original PCR/header/payload bytes.
			maxFrames := 0
			for _, l := range p.Layers {
				maxFrames = max(maxFrames, (95*l.Interleave+203)/204)
			}
			d.queueAt = [3]int{}
			for i, l := range p.Layers {
				delay := 204 * (maxFrames - (95*l.Interleave+203)/204)
				d.queues[i] = make([][]byte, delay)
				if l.Segments > 0 {
					if modulationBits(l.Constellation) == 0 {
						err = fmt.Errorf("unsupported constellation")
						return
					}
					d.layers[i] = newLayerDecoder(l, p.Mode)
				} else {
					d.layers[i] = nil
				}
			}
		}
		if d.p == nil || err != nil {
			return
		}
		if !d.started {
			if s.FrameSymbol != 0 {
				return
			}
			d.started = true
		}
		for i, k := range d.permutation {
			d.ordered[i] = s.Data[k]
		}
		at := 0
		for i, l := range d.layers {
			if l != nil {
				if len(d.queues[i]) == 0 {
					out = l.push(out, d.ordered[at:at+l.n])
				} else {
					q := d.queues[i]
					j := d.queueAt[i]
					out = append(out, q[j]...)
					q[j] = l.push(q[j][:0], d.ordered[at:at+l.n])
					d.queueAt[i] = (j + 1) % len(q)
				}
				at += l.n
			}
		}
	})
	return out, err
}
