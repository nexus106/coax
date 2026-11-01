// Copyright 2026 Till0196
// SPDX-License-Identifier: GPL-2.0-or-later
package msi2500

import (
	"bufio"
	"context"
	"encoding/binary"
	"github.com/Till0196/coax/internal/coaxd/tuner/msi001"
	"github.com/Till0196/coax/pkg/usbio"
	"os"
	"testing"
	"time"
)

// TestHardwareCapture is opt-in: MSI2500_CAPTURE names an untracked cs16 output.
// It requires exclusive use of a connected 1df7:2500 device for ten seconds.
// MSI2500_CAPTURE_SECOND optionally records ten more seconds after a fresh
// Close/Open/New, for RF waveform comparison rather than read-count checks.
func TestHardwareCapture(t *testing.T) {
	path := os.Getenv("MSI2500_CAPTURE")
	if path == "" {
		t.Skip("set MSI2500_CAPTURE to capture hardware IQ")
	}
	tr, err := usbio.Open("", []usbio.ID{{Vendor: 0x1df7, Product: 0x2500}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(tr)
	if err != nil {
		tr.Close()
		t.Fatal(err)
	}
	defer b.Close()
	if err = msi001.New(b.TunerBus()).Tune(539142857, 15); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)
	if err = b.Start(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	const target = SampleRate * 10
	samples, loss := 0, 0
	var iq [336]complex64
	var raw [1344]byte
	var power float64
	for samples < target {
		n, gap, e := b.ReadIQ(ctx, iq[:])
		if e != nil {
			t.Fatal(e)
		}
		if gap {
			loss++
		}
		for j, v := range iq[:n] {
			i, q := int16(real(v)*32768), int16(imag(v)*32768)
			binary.LittleEndian.PutUint16(raw[j*4:], uint16(i))
			binary.LittleEndian.PutUint16(raw[j*4+2:], uint16(q))
			power += float64(real(v)*real(v) + imag(v)*imag(v))
		}
		if _, err = w.Write(raw[:n*4]); err != nil {
			t.Fatal(err)
		}
		samples += n
	}
	if err = b.Stop(); err != nil {
		t.Fatal(err)
	}
	if err = w.Flush(); err != nil {
		t.Fatal(err)
	}
	t.Logf("path=%s frequency=539142857 rate=%d gain=15 samples=%d gaps=%d wall=%s meanPower=%g", path, SampleRate, samples, loss, time.Since(start), power/float64(samples))
	// Verify stopped/restarted endpoint and ADC work without reopening.
	if err = b.Start(); err != nil {
		t.Fatal(err)
	}
	if n, _, err := b.ReadIQ(ctx, iq[:]); err != nil || n == 0 {
		t.Fatalf("restart n=%d err=%v", n, err)
	}
	if err = b.Close(); err != nil {
		t.Fatal(err)
	}
	// A fresh Bridge must reset/reacquire again after the previous session.
	tr2, e := usbio.Open("", []usbio.ID{{Vendor: 0x1df7, Product: 0x2500}})
	if e != nil {
		t.Fatal(e)
	}
	b2, e := New(tr2)
	if e != nil {
		tr2.Close()
		t.Fatal(e)
	}
	defer b2.Close()
	if e = msi001.New(b2.TunerBus()).Tune(539142857, 15); e != nil {
		t.Fatal(e)
	}
	if e = b2.Start(); e != nil {
		t.Fatal(e)
	}
	if secondPath := os.Getenv("MSI2500_CAPTURE_SECOND"); secondPath != "" {
		f2, e := os.Create(secondPath)
		if e != nil {
			t.Fatal(e)
		}
		defer f2.Close()
		w2 := bufio.NewWriterSize(f2, 1<<20)
		total, gaps := 0, 0
		var pwr float64
		start2 := time.Now()
		for total < target {
			n, gap, e := b2.ReadIQ(ctx, iq[:])
			if e != nil {
				t.Fatal(e)
			}
			if gap {
				gaps++
			}
			for j, v := range iq[:n] {
				binary.LittleEndian.PutUint16(raw[j*4:], uint16(int16(real(v)*32768)))
				binary.LittleEndian.PutUint16(raw[j*4+2:], uint16(int16(imag(v)*32768)))
				pwr += float64(real(v)*real(v) + imag(v)*imag(v))
			}
			if _, e = w2.Write(raw[:n*4]); e != nil {
				t.Fatal(e)
			}
			total += n
		}
		if e = w2.Flush(); e != nil {
			t.Fatal(e)
		}
		t.Logf("second path=%s samples=%d gaps=%d wall=%s meanPower=%g", secondPath, total, gaps, time.Since(start2), pwr/float64(total))
	} else if n, _, e := b2.ReadIQ(ctx, iq[:]); e != nil || n == 0 {
		t.Fatalf("fresh reopen n=%d err=%v", n, e)
	}

}
