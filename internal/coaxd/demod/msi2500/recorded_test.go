// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package msi2500

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"testing"
	"time"
)

// Recordings remain local and are never included in the repository.
func TestRecordedIQ(t *testing.T) {
	name := os.Getenv("COAX_ISDBT_IQ")
	if name == "" {
		t.Skip("set COAX_ISDBT_IQ to a local interleaved signed16 IQ recording")
	}
	input, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	var out *os.File
	if name := os.Getenv("COAX_ISDBT_SYMBOLS"); name != "" {
		out, err = os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
	}
	sync := NewSynchronizer()
	raw := make([]byte, 4*32768)
	samples := make([]complex64, 32768)
	count := 0
	start := time.Now()
	var frames int
	for {
		n, err := io.ReadFull(input, raw)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			t.Fatal(err)
		}
		if n%4 != 0 {
			t.Fatal("partial IQ sample")
		}
		for i := 0; i < n/4; i++ {
			samples[i] = complex(float32(int16(binary.LittleEndian.Uint16(raw[4*i:])))/32768, float32(int16(binary.LittleEndian.Uint16(raw[4*i+2:])))/32768)
		}
		count += n / 4
		sync.PushIQ(samples[:n/4], false, func(s Symbol) {
			if s.Parameters != nil {
				frames++
			}
			if out != nil && sync.Snapshot().Parameters != nil {
				b := make([]byte, len(s.Data)*8)
				for i, c := range s.Data {
					binary.LittleEndian.PutUint32(b[i*8:], math.Float32bits(real(c)))
					binary.LittleEndian.PutUint32(b[i*8+4:], math.Float32bits(imag(c)))
				}
				if _, e := out.Write(b); e != nil {
					t.Fatal(e)
				}
			}
		})
		if err != nil {
			break
		}
	}
	snap := sync.Snapshot()
	t.Logf("samples=%d duration=%.3fs process=%s frames=%d sync=%+v", count, float64(count)/8126984, time.Since(start), frames, snap)
	if frames < 2 || snap.Parameters == nil {
		t.Fatal("no sustained verified TMCC")
	}
	t.Logf("parameters=%+v", *snap.Parameters)
}

// Local recordings are optional and are never distributed with the source.
// Check packet continuity after pipeline fill, independently of B25 or video.
func TestRecordedSamplingClockContinuity(t *testing.T) {
	name := os.Getenv("COAX_CLOCK_IQ")
	if name == "" {
		t.Skip("set COAX_CLOCK_IQ to a local signed16 IQ recording")
	}
	input, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	d, _ := New(Config{})
	raw := make([]byte, 4*32768)
	samples := make([]complex64, 32768)
	last := make(map[int]byte)
	packets, failures, total := 0, 0, 0
	for {
		n, err := io.ReadFull(input, raw)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			t.Fatal(err)
		}
		for i := 0; i < n/4; i++ {
			samples[i] = complex(float32(int16(binary.LittleEndian.Uint16(raw[4*i:])))/32768, float32(int16(binary.LittleEndian.Uint16(raw[4*i+2:])))/32768)
		}
		ts, e := d.PushIQ(samples[:n/4], false)
		if e != nil {
			t.Fatal(e)
		}
		total += n / 4
		if total > 8126984*2 {
			for i := 0; i+188 <= len(ts); i += 188 {
				p := ts[i : i+188]
				pid := int(p[1]&31)<<8 | int(p[2])
				cc := p[3] & 15
				if pid == 8191 || p[3]&16 == 0 {
					continue
				}
				if p[3]&32 != 0 && p[4] > 0 && p[5]&128 != 0 {
					delete(last, pid)
				}
				if prev, ok := last[pid]; ok && cc != (prev+1)%16 {
					failures++
				}
				last[pid] = cc
				packets++
			}
		}
		if err != nil {
			break
		}
	}
	t.Logf("packets=%d continuity_errors=%d snapshot=%+v", packets, failures, d.Snapshot())
	if packets < 10000 || failures > 10 {
		t.Fatalf("sustained transport corruption: %d errors / %d packets", failures, packets)
	}
}
