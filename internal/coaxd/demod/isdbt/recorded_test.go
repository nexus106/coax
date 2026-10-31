// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package isdbt

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
