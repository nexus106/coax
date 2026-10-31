// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package b25

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/Till0196/coax/pkg/mpegts"
	"io"
)

const maxPending = 8 * 1024 * 1024

// Decode preserves packet order, PSI, null packets and adaptation fields. An
// initial bounded queue waits for PAT/PMT/ECM. On failure queued packets remain
// scrambled and retain their original headers; the error marks a partial output.
func Decode(ctx context.Context, dst io.Writer, src io.Reader, card Card) error {
	if card == nil {
		return fmt.Errorf("B25 requires a card")
	}
	d := decoder{card: card, sections: map[uint16]*assembler{0: newAssembler()}, pmts: map[uint16]bool{}, programs: map[uint16]map[uint16]uint16{}, pids: map[uint16]uint16{}, ecms: map[uint16]*ecmKeys{}}
	d.system, d.iv = card.Parameters()
	defer func() {
		clear(d.system[:])
		clear(d.iv[:])
		for _, k := range d.ecms {
			k.clear()
		}
	}()
	type queued struct {
		data  []byte
		ready bool
	}
	var pending []queued
	write := func(p []byte) error {
		n, err := dst.Write(p)
		if err == nil && n != len(p) {
			err = io.ErrShortWrite
		}
		return err
	}
	fail := func(err error) error {
		for _, p := range pending {
			if werr := write(p.data); werr != nil {
				return fmt.Errorf("B25 partial output: %w", werr)
			}
		}
		return fmt.Errorf("B25 partial output: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		p := make([]byte, 188)
		n, err := io.ReadFull(src, p)
		if err == io.EOF {
			if len(pending) > 0 {
				return fail(ErrMissingECM)
			}
			return nil
		}
		if err != nil {
			if n > 0 {
				pending = append(pending, queued{data: p[:n]})
			}
			return fail(err)
		}
		pending = append(pending, queued{data: p})
		generation := d.generation
		if err := d.inspect(ctx, p); err != nil {
			return fail(err)
		}
		// Resolve every packet while its control words are current, even if an
		// earlier packet from another service still awaits its PMT or ECM.
		begin := len(pending) - 1
		if generation != d.generation {
			begin = 0
		}
		for i := begin; i < len(pending); i++ {
			if !pending[i].ready {
				var err error
				pending[i].ready, err = d.descramble(pending[i].data)
				if err != nil {
					return fail(err)
				}
			}
		}
		for len(pending) > 0 && pending[0].ready {
			if err := write(pending[0].data); err != nil {
				return fmt.Errorf("B25 partial output: %w", err)
			}
			pending[0] = queued{}
			pending = pending[1:]
		}
		if len(pending)*188 > maxPending {
			return fail(ErrMissingECM)
		}
	}
}

type ecmKeys struct {
	odd, even *multi2
	last      []byte
}
type decoder struct {
	card        Card
	system      [32]byte
	iv          [8]byte
	sections    map[uint16]*assembler
	pmts        map[uint16]bool
	programs    map[uint16]map[uint16]uint16
	pids        map[uint16]uint16
	generation  uint64
	patIdentity uint32
	patLast     byte
	patSections map[byte][]uint16
	ecms        map[uint16]*ecmKeys
}

func (d *decoder) inspect(ctx context.Context, p []byte) error {
	if p[0] != 0x47 || p[1]&0x80 != 0 {
		return fmt.Errorf("invalid transport packet")
	}
	afc := (p[3] >> 4) & 3
	if afc == 0 {
		return fmt.Errorf("reserved adaptation control")
	}
	pid, payload, start, ok := mpegts.Payload(p)
	if afc == 2 && (p[4] != 183 || p[3]&0xc0 != 0) || afc == 3 && p[4] > 182 {
		return fmt.Errorf("invalid adaptation field")
	}
	if !ok {
		return nil
	}
	a := d.sections[pid]
	if a == nil {
		return nil
	}
	if p[3]&0xc0 != 0 {
		return fmt.Errorf("scrambled PSI/ECM")
	}
	if afc == 3 && p[4] > 0 && p[5]&0x80 != 0 {
		a.seen = false
		a.data = nil
	}
	return a.feed(payload, start, p[3]&15, func(s []byte) error { return d.section(ctx, pid, s) })
}
func (d *decoder) descramble(p []byte) (bool, error) {
	kind := p[3] >> 6
	if kind == 0 {
		return true, nil
	}
	if kind == 1 {
		return false, fmt.Errorf("reserved scrambling control")
	}
	pid, payload, _, ok := mpegts.Payload(p)
	if !ok {
		return true, nil
	}
	ecm, found := d.pids[pid]
	if !found {
		return false, nil
	}
	keys := d.ecms[ecm]
	if keys == nil {
		return false, nil
	}
	cipher := keys.even
	if kind == 3 {
		cipher = keys.odd
	}
	cipher.decrypt(payload)
	p[3] &= 0x3f
	return true, nil
}
func caPID(p []byte) (uint16, bool, error) {
	for len(p) > 0 {
		if len(p) < 2 || int(p[1])+2 > len(p) {
			return 0, false, fmt.Errorf("invalid CA descriptor")
		}
		n := int(p[1]) + 2
		if p[0] == 9 && n >= 6 && binary.BigEndian.Uint16(p[2:]) == 5 {
			return binary.BigEndian.Uint16(p[4:]) & 8191, true, nil
		}
		p = p[n:]
	}
	return 0, false, nil
}
func (d *decoder) section(ctx context.Context, pid uint16, s []byte) error {
	if pid == 0 {
		if s[0] != 0 || len(s) < 12 {
			return fmt.Errorf("invalid PAT")
		}
		if s[5]&1 == 0 {
			return nil
		}
		identity := uint32(binary.BigEndian.Uint16(s[3:]))<<8 | uint32(s[5]&0x3e)
		if d.patSections == nil || d.patIdentity != identity {
			d.patIdentity = identity
			d.patSections = map[byte][]uint16{}
			d.patLast = s[7]
		}
		if s[6] > d.patLast || s[7] != d.patLast || (len(s)-12)%4 != 0 {
			return fmt.Errorf("invalid PAT section numbering")
		}
		var entries []uint16
		for off := 8; off+4 <= len(s)-4; off += 4 {
			if binary.BigEndian.Uint16(s[off:]) != 0 {
				entries = append(entries, binary.BigEndian.Uint16(s[off+2:])&8191)
			}
		}
		d.patSections[s[6]] = entries
		if len(d.patSections) == int(d.patLast)+1 {
			next := map[uint16]bool{}
			for _, entries := range d.patSections {
				for _, pmt := range entries {
					next[pmt] = true
					if d.sections[pmt] == nil {
						d.sections[pmt] = newAssembler()
					}
				}
			}
			for pmt := range d.pmts {
				if !next[pmt] {
					delete(d.sections, pmt)
					delete(d.programs, pmt)
				}
			}
			d.pmts = next
			d.rebuildPIDs()
		}
		return nil
	}
	if d.pmts[pid] {
		if s[0] != 2 || len(s) < 16 {
			return fmt.Errorf("invalid PMT")
		}
		if s[5]&1 == 0 {
			return nil
		}
		end := len(s) - 4
		n := int(binary.BigEndian.Uint16(s[10:]) & 4095)
		if 12+n > end {
			return fmt.Errorf("invalid PMT descriptors")
		}
		defaultECM, hasDefault, err := caPID(s[12 : 12+n])
		if err != nil {
			return err
		}
		entries := map[uint16]uint16{}
		for off := 12 + n; off < end; {
			if off+5 > end {
				return fmt.Errorf("truncated PMT stream")
			}
			es := binary.BigEndian.Uint16(s[off+1:]) & 8191
			dn := int(binary.BigEndian.Uint16(s[off+3:]) & 4095)
			if off+5+dn > end {
				return fmt.Errorf("invalid stream descriptors")
			}
			ecm, ok, err := caPID(s[off+5 : off+5+dn])
			if err != nil {
				return err
			}
			if !ok {
				ecm, ok = defaultECM, hasDefault
			}
			if ok {
				entries[es] = ecm
			}
			off += 5 + dn
		}
		d.programs[pid] = entries
		d.rebuildPIDs()
		return nil
	}
	if s[0] != 0x82 && s[0] != 0x83 {
		return nil
	}
	old := d.ecms[pid]
	if old != nil && bytes.Equal(old.last, s) {
		return nil
	}
	offset := 3
	if s[1]&0x80 != 0 {
		offset = 8
	}
	if len(s) < offset+4 {
		return fmt.Errorf("invalid ECM")
	}
	keys, err := d.card.ProcessECM(ctx, s[offset:len(s)-4])
	if err != nil {
		if old != nil {
			old.clear()
		}
		delete(d.ecms, pid)
		return err
	}
	if old != nil {
		old.clear()
	}
	d.generation++
	d.ecms[pid] = &ecmKeys{odd: newMulti2(d.system, keys.Odd, d.iv), even: newMulti2(d.system, keys.Even, d.iv), last: bytes.Clone(s)}
	clear(keys.Odd[:])
	clear(keys.Even[:])
	return nil
}

func (d *decoder) rebuildPIDs() {
	d.generation++
	clear(d.pids)
	activeECMs := map[uint16]bool{}
	for _, entries := range d.programs {
		for pid, ecm := range entries {
			d.pids[pid] = ecm
			activeECMs[ecm] = true
		}
	}
	// ECM PIDs belong to the current program maps, not the history of PMTs.
	// Preserve shared ECMs until their final service reference is removed.
	for pid := range d.sections {
		if pid != 0 && !d.pmts[pid] && !activeECMs[pid] {
			delete(d.sections, pid)
		}
	}
	for pid, keys := range d.ecms {
		if !activeECMs[pid] {
			keys.clear()
			delete(d.ecms, pid)
		}
	}
	for pid := range activeECMs {
		if d.sections[pid] == nil {
			d.sections[pid] = newAssembler()
		}
	}
}

func (k *ecmKeys) clear() {
	clear(k.odd.work[:])
	clear(k.even.work[:])
	clear(k.last)
}
