// Copyright 2026 Till0196
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package b25

import (
	"fmt"
	"github.com/Till0196/coax/pkg/mpegts"
)

type assembler struct {
	data []byte
	last byte
	seen bool
}

func newAssembler() *assembler { return &assembler{} }
func (a *assembler) feed(p []byte, start bool, cc byte, emit func([]byte) error) error {
	if a.seen {
		if cc == a.last {
			return nil
		}
		if cc != (a.last+1)&15 {
			a.data = nil
		}
	}
	a.last = cc
	a.seen = true
	if start {
		if len(p) == 0 {
			return fmt.Errorf("missing PSI pointer")
		}
		n := int(p[0])
		p = p[1:]
		if n > len(p) {
			return fmt.Errorf("invalid PSI pointer")
		}
		if len(a.data) > 0 {
			if err := a.add(p[:n], emit); err != nil {
				return err
			}
		}
		a.data = nil
		p = p[n:]
	} else if len(a.data) == 0 {
		return nil
	}
	return a.add(p, emit)
}
func (a *assembler) add(p []byte, emit func([]byte) error) error {
	for len(p) > 0 {
		if len(a.data) == 0 && p[0] == 255 {
			return nil
		}
		need := 3 - len(a.data)
		if need > 0 {
			if need > len(p) {
				need = len(p)
			}
			a.data = append(a.data, p[:need]...)
			p = p[need:]
			if len(a.data) < 3 {
				return nil
			}
		}
		n := 3 + int(a.data[1]&15)<<8 + int(a.data[2])
		if n < 7 || n > 4096 {
			a.data = nil
			return fmt.Errorf("invalid section size")
		}
		need = n - len(a.data)
		if need > len(p) {
			need = len(p)
		}
		a.data = append(a.data, p[:need]...)
		p = p[need:]
		if len(a.data) < n {
			return nil
		}
		if !mpegts.ValidSection(a.data) {
			a.data = nil
			return fmt.Errorf("PSI/ECM CRC mismatch")
		}
		if err := emit(a.data); err != nil {
			return err
		}
		a.data = nil
	}
	return nil
}
