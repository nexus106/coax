// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package fec

import (
	"bytes"
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

// This reference encoder follows the delay taps drawn in STD-B31 Fig.3-10
// directly (current input first), independently of the decoder's trellis.
func encodeBits(data []byte, rate int) []byte {
	xp := []string{"1", "10", "101", "10101", "1000101"}[rate]
	yp := []string{"1", "11", "110", "11010", "1111010"}[rate]
	var delay [7]byte
	var out []byte
	n := 0
	for _, v := range data {
		for bit := 7; bit >= 0; bit-- {
			copy(delay[1:], delay[:6])
			delay[0] = (v >> bit) & 1
			x := delay[0] ^ delay[1] ^ delay[2] ^ delay[3] ^ delay[6]
			y := delay[0] ^ delay[2] ^ delay[3] ^ delay[5] ^ delay[6]
			if xp[n%len(xp)] == '1' {
				out = append(out, x)
			}
			if yp[n%len(yp)] == '1' {
				out = append(out, y)
			}
			n++
		}
	}
	return out
}
func TestStandardImpulse(t *testing.T) {
	want := []byte{1, 1, 1, 0, 1, 1, 1, 1, 0, 0, 0, 1, 1, 1, 0, 0}
	if got := encodeBits([]byte{0x80}, 0); !bytes.Equal(got, want) {
		t.Fatalf("reference tap error: %v", got)
	}
	// The decoded impulse must survive the non-palindromic generator ordering.
	data := make([]byte, 128)
	data[0] = 0x80
	v, _ := NewViterbi(0)
	got := v.Decode(nil, encodeBits(data, 0))
	if len(got) == 0 || !bytes.Equal(got, data[:len(got)]) {
		t.Fatalf("impulse decoded %x", got)
	}
}
func TestViterbiStreamingRates(t *testing.T) {
	data := make([]byte, 4096)
	rand.New(rand.NewSource(81)).Read(data)
	for rate := 0; rate < 5; rate++ {
		t.Run(fmt.Sprint(rate), func(t *testing.T) {
			coded := encodeBits(data, rate)
			v, err := NewViterbi(rate)
			if err != nil {
				t.Fatal(err)
			}
			full := v.Decode([]byte{0xaa}, coded)
			if len(full) < len(data)-32 {
				t.Fatalf("excessive latency %d", len(full))
			}
			if full[0] != 0xaa || !bytes.Equal(full[1:], data[:len(full)-1]) {
				t.Fatal("decoded bytes differ")
			}
			v.Reset()
			var chunked []byte
			for n := 0; n < len(coded); {
				k := 1 + n%37
				if k > len(coded)-n {
					k = len(coded) - n
				}
				chunked = v.Decode(chunked, coded[n:n+k])
				n += k
			}
			if !bytes.Equal(chunked, full[1:]) {
				t.Fatal("chunk boundary changes output")
			}
			v.Reset()
			if again := v.Decode(nil, coded); !bytes.Equal(again, chunked) {
				t.Fatal("reset differs")
			}
		})
	}
	for _, r := range []int{-1, 5} {
		if _, err := NewViterbi(r); err == nil {
			t.Fatal("invalid rate accepted")
		}
	}
}
func TestViterbiCorrectsHardBitErrors(t *testing.T) {
	data := make([]byte, 2048)
	rand.New(rand.NewSource(5)).Read(data)
	for r := 0; r < 5; r++ {
		coded := encodeBits(data, r)
		for i := 200; i < len(coded); i += 503 {
			coded[i] ^= 1
		}
		v, _ := NewViterbi(r)
		got := v.Decode(nil, coded)
		if len(got) == 0 || !bytes.Equal(got, data[:len(got)]) {
			t.Fatalf("rate %d hard error not corrected", r)
		}
	}
}
func TestViterbiLatency(t *testing.T) {
	v, _ := NewViterbi(0)
	if got := v.Decode(nil, make([]byte, 254)); len(got) != 0 {
		t.Fatalf("early output %d", len(got))
	}
	if got := v.Decode(nil, []byte{0, 0}); len(got) != 4 {
		t.Fatalf("first output bytes %d", len(got))
	}
}

// Slow carryless multiplication and systematic polynomial division provide
// independent encoded vectors, without using production GF tables or decoder.
func refMul(a, b byte) byte {
	v := 0
	x := int(a)
	for b != 0 {
		if b&1 != 0 {
			v ^= x
		}
		b >>= 1
		x <<= 1
		if x&256 != 0 {
			x ^= 0x11d
		}
	}
	return byte(v)
}
func rsEncode(data []byte) []byte {
	g := []byte{1}
	root := byte(1)
	for i := 0; i < 16; i++ {
		next := make([]byte, len(g)+1)
		for j, v := range g {
			next[j] ^= v
			next[j+1] ^= refMul(v, root)
		}
		g = next
		root = refMul(root, 2)
	}
	word := make([]byte, 204)
	copy(word, data)
	for i := 0; i < 188; i++ {
		v := word[i]
		for j := 1; j <= 16; j++ {
			word[i+j] ^= refMul(v, g[j])
		}
	}
	copy(word, data)
	return word
}
func TestRSKnownVectorAndEightErrors(t *testing.T) {
	data := make([]byte, 188)
	data[187] = 1
	original := rsEncode(data)
	// Generator coefficients for product (x+alpha^0)..(x+alpha^15).
	parity := []byte{59, 13, 104, 189, 68, 209, 30, 8, 163, 65, 41, 229, 98, 50, 36, 59}
	if !bytes.Equal(original[188:], parity) {
		t.Fatalf("reference parity %v", original[188:])
	}
	for n := 0; n <= 8; n++ {
		word := bytes.Clone(original)
		for i := 0; i < n; i++ {
			word[(i*29)%204] ^= byte(i + 17)
		}
		got, err := DecodeRS(word)
		if err != nil || got != n || !bytes.Equal(word, original) {
			t.Fatalf("errors=%d corrected=%d err=%v", n, got, err)
		}
	}
}
func TestRSRandomPackets(t *testing.T) {
	rng := rand.New(rand.NewSource(81))
	for n := 0; n < 100; n++ {
		data := make([]byte, 188)
		rng.Read(data)
		original := rsEncode(data)
		word := bytes.Clone(original)
		positions := rng.Perm(204)
		for i := 0; i < 8; i++ {
			word[positions[i]] ^= byte(1 + rng.Intn(255))
		}
		c, err := DecodeRS(word)
		if err != nil || c != 8 || !bytes.Equal(word, original) {
			t.Fatalf("packet %d count %d: %v", n, c, err)
		}
	}
}
func TestRSRejectsNineErrorsAndWrongLength(t *testing.T) {
	word := make([]byte, 204)
	for i := 0; i < 9; i++ {
		word[i*23] = byte(1 + i)
	}
	before := bytes.Clone(word)
	if _, err := DecodeRS(word); err == nil {
		t.Fatal("nine-error vector accepted")
	}
	if !bytes.Equal(word, before) {
		t.Fatal("failed decode mutated input")
	}
	for _, n := range []int{0, 188, 203, 205} {
		if _, err := DecodeRS(make([]byte, n)); err == nil {
			t.Fatalf("length %d accepted", n)
		}
	}
}
func refInterleave(src []byte) []byte {
	out := make([]byte, len(src))
	for i, v := range src {
		j := i + 12*17*(i%12)
		if j < len(out) {
			out[j] = v
		}
	}
	return out
}
func TestByteDeinterleaverDelayAndReset(t *testing.T) {
	data := make([]byte, 10000)
	rand.New(rand.NewSource(17)).Read(data)
	coded := refInterleave(data)
	d := NewByteDeinterleaver()
	out := d.Push(nil, coded)
	const delay = 12 * 17 * 11
	if len(out) != len(data) || !bytes.Equal(out[delay:], data[:len(data)-delay]) {
		t.Fatal("inverse branch delay or order differs")
	}
	d.Reset()
	var chunked []byte
	for i := 0; i < len(coded); i += 7 {
		end := min(i+7, len(coded))
		chunked = d.Push(chunked, coded[i:end])
	}
	if !bytes.Equal(out, chunked) {
		t.Fatal("reset/chunked interleaver differs")
	}
	d.ResetPhase(5)
	in := make([]byte, 12*17*6+1)
	in[0] = 99
	out = d.Push(nil, in)
	if out[12*17*6] != 99 {
		t.Fatal("phase 5 must have six branch delays")
	}
}
func TestIndependentInstances(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := make([]byte, 512)
			v, _ := NewViterbi(2)
			v.Decode(nil, encodeBits(data, 2))
			DecodeRS(make([]byte, 204))
			NewByteDeinterleaver().Push(nil, data)
		}()
	}
	wg.Wait()
}
func BenchmarkViterbi(b *testing.B) {
	for r := 0; r < 5; r++ {
		b.Run(fmt.Sprint(r), func(b *testing.B) {
			v, _ := NewViterbi(r)
			data := make([]byte, 4200)
			rand.New(rand.NewSource(73)).Read(data)
			// 4200 bytes is an integer number of all puncturing periods;
			// a zero final byte returns the reference encoder to state zero.
			data[len(data)-1] = 0
			coded := encodeBits(data, r)
			out := make([]byte, 0, 4204)
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out = v.Decode(out[:0], coded)
			}
		})
	}
}
func BenchmarkRS(b *testing.B) {
	data := make([]byte, 188)
	rand.New(rand.NewSource(5)).Read(data)
	original := rsEncode(data)
	for _, errors := range []int{0, 8} {
		b.Run(fmt.Sprint(errors), func(b *testing.B) {
			var word [204]byte
			b.SetBytes(188)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				copy(word[:], original)
				for j := 0; j < errors; j++ {
					word[j*23] ^= byte(j + 1)
				}
				if _, err := DecodeRS(word[:]); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
func BenchmarkByteDeinterleaver(b *testing.B) {
	d := NewByteDeinterleaver()
	in := make([]byte, 4096)
	out := make([]byte, 0, len(in))
	b.SetBytes(int64(len(in)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out = d.Push(out[:0], in)
	}
}

// Compare the optimized recurrence against polynomial multiplication directly.
func TestSyndromesPolynomialOracle(t *testing.T) {
	mul := func(a, b byte) byte {
		var v byte
		for b != 0 {
			if b&1 != 0 {
				v ^= a
			}
			high := a & 128
			a <<= 1
			if high != 0 {
				a ^= 0x1d
			}
			b >>= 1
		}
		return v
	}
	r := rand.New(rand.NewSource(73))
	for trial := 0; trial < 300; trial++ {
		word := make([]byte, 204)
		r.Read(word)
		var want [16]byte
		root := byte(1)
		for i := range want {
			for _, c := range word {
				want[i] = mul(want[i], root) ^ c
			}
			root = mul(root, 2)
		}
		if got := syndromes(word); got != want {
			t.Fatalf("trial%d: got%x want%x", trial, got, want)
		}
	}
}
