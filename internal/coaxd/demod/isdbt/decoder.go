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
	generation  uint64
}

func New(Config) (*Decoder, error)        { return &Decoder{sync: NewSynchronizer()}, nil }
func (d *Decoder) Snapshot() SyncSnapshot { return d.sync.Snapshot() }

// Locked requires verified TMCC and at least five RS-valid packets on every
// active layer. Sixteen consecutive failed words revoke the transport lock.
func (d *Decoder) Locked() bool {
	if d.p == nil || d.generation != d.sync.Generation() || d.sync.Err() != nil {
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
	if discontinuity || d.generation != d.sync.Generation() {
		d.resetPipeline()
	}
	var out []byte
	// Reserve complete packet slots for the maximum symbols in this chunk.
	// Return storage belongs to the caller; no alias is reused by the decoder.
	if d.p != nil {
		period := (1 << (10 + d.p.Mode)) * (d.p.GuardDenominator + 1) / d.p.GuardDenominator
		symbols := (len(samples)+len(d.sync.buffer))/period + 1
		capacity := 0
		for _, l := range d.layers {
			if l != nil {
				capacity += (l.frameBytes/204/204 + 1) * 188
			}
		}
		out = make([]byte, 0, symbols*capacity)
	}
	var err error
	d.sync.PushIQ(samples, discontinuity, func(symbol Symbol) {
		var nextErr error
		out, nextErr = d.consumeSymbol(out, symbol)
		if nextErr != nil {
			err = nextErr
		}
	})
	if d.generation != d.sync.Generation() {
		d.resetPipeline()
	}

	if err == nil {
		err = d.sync.Err()
	}
	return out, err
}

func (d *Decoder) resetPipeline() {
	d.p = nil
	d.started = false
	d.layers = [3]*layerDecoder{}
	d.queues = [3][][]byte{}
	d.queueAt = [3]int{}
	d.generation = d.sync.Generation()
}
func (d *Decoder) consumeSymbol(out []byte, s Symbol) ([]byte, error) {
	if d.generation != d.sync.Generation() {
		d.resetPipeline()
	}
	var err error
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
					return out, err
				}
				d.layers[i] = newLayerDecoder(l, p.Mode)
			} else {
				d.layers[i] = nil
			}
		}
	}
	if d.p == nil || err != nil {
		return out, err
	}
	if !d.started {
		if s.FrameSymbol != 0 {
			return out, err
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
	return out, err
}
