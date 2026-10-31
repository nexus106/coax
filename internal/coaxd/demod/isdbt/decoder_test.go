// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package isdbt

import (
	"encoding/binary"
	"io"
	"os"
	"testing"
)

func TestRecordedTS(t *testing.T) {
	name := os.Getenv("COAX_ISDBT_IQ")
	if name == "" {
		t.Skip("set COAX_ISDBT_IQ")
	}
	f, e := os.Open(name)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	d, _ := New(Config{})
	raw := make([]byte, 4*32768)
	iq := make([]complex64, 32768)
	var output []byte
	for {
		n, e := io.ReadFull(f, raw)
		for i := 0; i < n/4; i++ {
			iq[i] = complex(float32(int16(binary.LittleEndian.Uint16(raw[4*i:])))/32768, float32(int16(binary.LittleEndian.Uint16(raw[4*i+2:])))/32768)
		}
		ts, err := d.PushIQ(iq[:n/4], false)
		if err != nil {
			t.Fatal(err)
		}
		output = append(output, ts...)
		if e != nil {
			if e != io.EOF && e != io.ErrUnexpectedEOF {
				t.Fatal(e)
			}
			if n%4 != 0 {
				t.Fatal("partial IQ sample")
			}
			break
		}
	}
	for i, l := range d.layers {
		if l != nil {
			t.Logf("layer%d packets=%d failures=%d bytes=%d", i, l.packets, l.failures, l.position)
		}
	}
	if path := os.Getenv("COAX_ISDBT_TS"); path != "" {
		if e := os.WriteFile(path, output, 0600); e != nil {
			t.Fatal(e)
		}
	}
	for _, pid := range []int{0, 0x10, 0x11, 0x100, 0x110, 0x581, 0x583} {
		found := false
		for pos := 0; pos+188 <= len(output); pos += 188 {
			p := output[pos : pos+188]
			if int(p[1]&31)*256+int(p[2]) == pid {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing SI/media PID%x", pid)
		}
	}
	if reference := os.Getenv("COAX_ISDBT_REFERENCE_TS"); reference != "" {
		r, e := os.ReadFile(reference)
		if e != nil {
			t.Fatal(e)
		}
		packets := map[string]bool{}
		for i := 0; i+188 <= len(r); i += 188 {
			p := r[i : i+188]
			if int(p[1]&31)*256+int(p[2]) == 0x100 {
				packets[string(p)] = true
			}
		}
		matched, total := 0, 0
		for i := 0; i+188 <= len(output); i += 188 {
			p := output[i : i+188]
			if int(p[1]&31)*256+int(p[2]) == 0x100 {
				total++
				if packets[string(p)] {
					matched++
				}
			}
		}
		t.Logf("reference MPEG2 PID0x100 exact packets=%d/%d", matched, total)
		if total < 1000 || matched*100 < total*97 {
			t.Fatal("native payload differs from independent reference")
		}
	}
	for _, pid := range []int{0, 0x10, 0x11, 0x1f0} {
		if !hasValidSection(output, pid) {
			t.Fatalf("no CRC-valid PSI section PID%x", pid)
		}
	}
	if !d.Locked() {
		t.Fatal("no validated RS/TS lock")
	}
	t.Logf("TS bytes=%d", len(output))
}

// Reassemble complete sections rather than accepting a PID or sync-byte cadence.
func hasValidSection(ts []byte, pid int) bool {
	var section []byte
	for i := 0; i+188 <= len(ts); i += 188 {
		p := ts[i : i+188]
		if int(p[1]&31)*256+int(p[2]) != pid || p[3]&16 == 0 {
			continue
		}
		at := 4
		if p[3]&32 != 0 {
			at += 1 + int(p[4])
		}
		if at >= 188 {
			continue
		}
		if p[1]&64 != 0 {
			at += 1 + int(p[at])
			section = nil
		}
		if at > 188 {
			continue
		}
		section = append(section, p[at:]...)
		for len(section) >= 3 {
			if section[0] == 255 {
				section = nil
				break
			}
			n := 3 + int(section[1]&15)*256 + int(section[2])
			if n < 7 || n > 4096 {
				section = nil
				break
			}
			if len(section) < n {
				break
			}
			crc := uint32(0xffffffff)
			for _, b := range section[:n] {
				crc ^= uint32(b) << 24
				for k := 0; k < 8; k++ {
					if crc&0x80000000 != 0 {
						crc = crc<<1 ^ 0x04c11db7
					} else {
						crc <<= 1
					}
				}
			}
			if crc == 0 {
				return true
			}
			section = section[n:]
		}
	}
	return false
}
