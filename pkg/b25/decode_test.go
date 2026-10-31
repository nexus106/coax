// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later
package b25

import (
	"bytes"
	"context"
	"errors"
	"github.com/Till0196/coax/pkg/mpegts"
	"io"
	"testing"
)

type testCard struct {
	keys  Keys
	calls int
	err   error
}

func (c *testCard) Parameters() ([32]byte, [8]byte)                  { return [32]byte{}, [8]byte{} }
func (c *testCard) ProcessECM(context.Context, []byte) (Keys, error) { c.calls++; return c.keys, c.err }
func (c *testCard) Close() error                                     { return nil }
func section(p []byte) []byte {
	n := len(p) - 3 + 4
	p[1] |= byte(n >> 8)
	p[2] = byte(n)
	crc := mpegts.CRC32(p)
	return append(p, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))
}
func packet(pid int, p []byte, start bool, cc byte) []byte {
	b := bytes.Repeat([]byte{255}, 188)
	b[0] = 0x47
	b[1] = byte(pid >> 8)
	if start {
		b[1] |= 0x40
	}
	b[2] = byte(pid)
	b[3] = 0x10 | cc
	copy(b[4:], p)
	return b
}
func tables() []byte {
	pat := section([]byte{0, 0xb0, 0, 0, 1, 0xc1, 0, 0, 0, 1, 0xe1, 0})
	pmt := section([]byte{2, 0xb0, 0, 0, 1, 0xc1, 0, 0, 0xe1, 1, 0xf0, 6, 9, 4, 0, 5, 0xe1, 0x20, 0x1b, 0xe1, 1, 0xf0, 0})
	return append(packet(0, append([]byte{0}, pat...), true, 0), packet(256, append([]byte{0}, pmt...), true, 0)...)
}
func TestDecodeWaitsForECMAndRotatesKeys(t *testing.T) {
	c := &testCard{}
	c.keys.Odd[0] = 3
	c.keys.Even[0] = 7
	src := tables()
	var want []byte
	want = append(want, src...)
	for i := 0; i < 2; i++ {
		ecm := packet(288, append([]byte{0}, section([]byte{0x82, 0x30, 0, byte(i)})...), true, byte(i))
		p := packet(257, bytes.Repeat([]byte{byte(i + 1)}, 184), false, byte(i))
		want = append(want, p...)
		if i == 0 {
			newMulti2([32]byte{}, c.keys.Even, [8]byte{}).encrypt(p[4:])
			p[3] |= 0x80
		} else {
			newMulti2([32]byte{}, c.keys.Odd, [8]byte{}).encrypt(p[4:])
			p[3] |= 0xc0
		}
		src = append(src, p...)
		src = append(src, ecm...)
		want = append(want, ecm...)
	}
	var dst bytes.Buffer
	if err := Decode(context.Background(), &dst, bytes.NewReader(src), c); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst.Bytes(), want) {
		t.Fatal("decrypted stream mismatch")
	}
	if c.calls != 2 {
		t.Fatalf("ECM calls %d", c.calls)
	}
}
func TestMissingECMPreservesPacket(t *testing.T) {
	p := packet(257, make([]byte, 184), false, 0)
	p[3] |= 0x80
	var dst bytes.Buffer
	err := Decode(context.Background(), &dst, bytes.NewReader(p), &testCard{})
	if !errors.Is(err, ErrMissingECM) {
		t.Fatalf("missing ECM: %v", err)
	}
	if !bytes.Equal(p, dst.Bytes()) {
		t.Fatal("failed packet altered")
	}
}
func TestSplitSection(t *testing.T) {
	s := section(append([]byte{0x82, 0x30, 0}, bytes.Repeat([]byte{0x11}, 190)...))
	a := newAssembler()
	var got [][]byte
	emit := func(p []byte) error { got = append(got, bytes.Clone(p)); return nil }
	if err := a.feed(append([]byte{0}, s[:100]...), true, 0, emit); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("premature section")
	}
	if err := a.feed(s[100:], false, 1, emit); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !bytes.Equal(got[0], s) {
		t.Fatal("split PSI lost")
	}
}

func TestPATReplacementDropsRemovedProgram(t *testing.T) {
	d := decoder{card: &testCard{}, sections: map[uint16]*assembler{0: newAssembler()}, pmts: map[uint16]bool{}, programs: map[uint16]map[uint16]uint16{}, pids: map[uint16]uint16{}, ecms: map[uint16]*ecmKeys{}}
	data := tables()
	for i := 0; i < len(data); i += 188 {
		if err := d.inspect(context.Background(), data[i:i+188]); err != nil {
			t.Fatal(err)
		}
	}
	replacement := section([]byte{0, 0xb0, 0, 0, 1, 0xc3, 0, 0, 0, 2, 0xe2, 0})
	if err := d.inspect(context.Background(), packet(0, append([]byte{0}, replacement...), true, 1)); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.pids[257]; ok {
		t.Fatal("removed program retained stale ECM mapping")
	}
}

func TestCardFailurePreservesScrambledPacket(t *testing.T) {
	for _, cause := range []error{ErrRefused, errors.New("removed card")} {
		src := tables()
		p := packet(257, make([]byte, 184), false, 0)
		p[3] |= 0x80
		src = append(src, p...)
		src = append(src, packet(288, append([]byte{0}, section([]byte{0x82, 0x30, 0, 1})...), true, 0)...)
		var dst bytes.Buffer
		err := Decode(context.Background(), &dst, bytes.NewReader(src), &testCard{err: cause})
		if !errors.Is(err, cause) {
			t.Fatalf("error %v", err)
		}
		if !bytes.Equal(dst.Bytes(), src) {
			t.Fatal("failure changed original stream")
		}
	}
}

func TestAdaptationAndTruncatedPackets(t *testing.T) {
	src := tables()
	ecm := packet(288, append([]byte{0}, section([]byte{0x82, 0x30, 0, 1})...), true, 0)
	src = append(src, ecm...)
	p := packet(257, bytes.Repeat([]byte{0x21}, 184), false, 0)
	p[3] = 0x30
	p[4] = 7
	want := append(bytes.Clone(src), p...)
	newMulti2([32]byte{}, [8]byte{}, [8]byte{}).encrypt(p[12:])
	p[3] |= 0x80
	src = append(src, p...)
	var dst bytes.Buffer
	if err := Decode(context.Background(), &dst, bytes.NewReader(src), &testCard{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst.Bytes(), want) {
		t.Fatal("adaptation changed")
	}
	dst.Reset()
	truncated := src[:len(src)-1]
	if err := Decode(context.Background(), &dst, bytes.NewReader(truncated), &testCard{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated: %v", err)
	}
	if !bytes.Equal(dst.Bytes(), truncated) {
		t.Fatal("truncated original lost")
	}
}

type rotatingCard struct {
	testCard
	sequence []Keys
}

func (c *rotatingCard) ProcessECM(context.Context, []byte) (Keys, error) {
	k := c.sequence[c.calls]
	c.calls++
	return k, nil
}
func TestBlockedProgramDoesNotDelayOtherKeyRotation(t *testing.T) {
	var first, second Keys
	first.Even[0] = 11
	second.Even[0] = 22
	c := &rotatingCard{sequence: []Keys{first, second}}
	src := tables()
	want := bytes.Clone(src)
	add := func(p []byte) { src = append(src, p...); want = append(want, p...) }
	ecm := func(i byte) []byte {
		return packet(288, append([]byte{0}, section([]byte{0x82, 0x30, 0, i})...), true, i)
	}
	add(ecm(0))
	encrypted := func(pid int, k Keys, cc byte) ([]byte, []byte) {
		p := packet(pid, bytes.Repeat([]byte{cc}, 184), false, cc)
		q := bytes.Clone(p)
		newMulti2([32]byte{}, k.Even, [8]byte{}).encrypt(q[4:])
		q[3] |= 0x80
		return p, q
	}
	plain, crypt := encrypted(258, second, 0)
	src = append(src, crypt...)
	want = append(want, plain...)
	plain, crypt = encrypted(257, first, 0)
	src = append(src, crypt...)
	want = append(want, plain...)
	add(ecm(1))
	plain, crypt = encrypted(257, second, 1)
	src = append(src, crypt...)
	want = append(want, plain...)
	pmt := section([]byte{2, 0xb0, 0, 0, 1, 0xc3, 0, 0, 0xe1, 1, 0xf0, 6, 9, 4, 0, 5, 0xe1, 0x20, 0x1b, 0xe1, 1, 0xf0, 0, 0x1b, 0xe1, 2, 0xf0, 0})
	add(packet(256, append([]byte{0}, pmt...), true, 1))
	var dst bytes.Buffer
	if err := Decode(context.Background(), &dst, bytes.NewReader(src), c); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(dst.Bytes(), want) {
		t.Fatal("blocked program caused another program to use later control words")
	}
}
func TestMalformedAdaptationFailsWithoutChangingPacket(t *testing.T) {
	for _, afc := range []byte{0x20, 0x30} {
		p := packet(257, nil, false, 0)
		p[3] = afc
		p[4] = 184
		var dst bytes.Buffer
		if err := Decode(context.Background(), &dst, bytes.NewReader(p), &testCard{}); err == nil {
			t.Fatal("accepted oversized adaptation field")
		}
		if !bytes.Equal(dst.Bytes(), p) {
			t.Fatal("malformed packet changed")
		}
	}
}

func testPMT(program, ecm, es int, version byte) []byte {
	return section([]byte{2, 0xb0, 0, byte(program >> 8), byte(program), 0xc1 | version<<1, 0, 0, byte(0xe0 | es>>8), byte(es), 0xf0, 6, 9, 4, 0, 5, byte(0xe0 | ecm>>8), byte(ecm), 0x1b, byte(0xe0 | es>>8), byte(es), 0xf0, 0})
}
func testECM(pid int, cc byte) []byte {
	return packet(pid, append([]byte{0}, section([]byte{0x82, 0x30, 0, cc})...), true, cc)
}
func TestRetiredECMPIDCanBecomeVideo(t *testing.T) {
	src := tables()
	src = append(src, testECM(288, 0)...)
	src = append(src, packet(256, append([]byte{0}, testPMT(1, 289, 288, 1)...), true, 1)...)
	src = append(src, testECM(289, 0)...)
	p := packet(288, bytes.Repeat([]byte{0x31}, 184), false, 0)
	want := append(bytes.Clone(src), p...)
	newMulti2([32]byte{}, [8]byte{}, [8]byte{}).encrypt(p[4:])
	p[3] |= 0x80
	src = append(src, p...)
	var dst bytes.Buffer
	if err := Decode(context.Background(), &dst, bytes.NewReader(src), &testCard{}); err != nil {
		t.Fatalf("valid reassigned video rejected: %v", err)
	}
	if !bytes.Equal(dst.Bytes(), want) {
		t.Fatal("reassigned video decoded incorrectly")
	}
}
func TestRetiredECMTrafficIsNotSentToCard(t *testing.T) {
	for _, change := range []string{"PAT", "PMT"} {
		t.Run(change, func(t *testing.T) {
			src := tables()
			src = append(src, testECM(288, 0)...)
			if change == "PAT" {
				pat := section([]byte{0, 0xb0, 0, 0, 1, 0xc3, 0, 0})
				src = append(src, packet(0, append([]byte{0}, pat...), true, 1)...)
			} else {
				src = append(src, packet(256, append([]byte{0}, testPMT(1, 289, 257, 1)...), true, 1)...)
			}
			src = append(src, testECM(288, 1)...)
			c := &testCard{}
			var dst bytes.Buffer
			if err := Decode(context.Background(), &dst, bytes.NewReader(src), c); err != nil {
				t.Fatal(err)
			}
			if c.calls != 1 {
				t.Fatalf("obsolete ECM caused %d card calls", c.calls)
			}
			if !bytes.Equal(dst.Bytes(), src) {
				t.Fatal("metadata stream changed")
			}
		})
	}
}
func TestSharedECMRetiredOnlyAfterLastReferenceAndReuseNeedsNewKeys(t *testing.T) {
	c := &testCard{}
	d := decoder{card: c, sections: map[uint16]*assembler{0: newAssembler()}, pmts: map[uint16]bool{}, programs: map[uint16]map[uint16]uint16{}, pids: map[uint16]uint16{}, ecms: map[uint16]*ecmKeys{}}
	feed := func(p []byte) {
		t.Helper()
		if err := d.inspect(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	pat := section([]byte{0, 0xb0, 0, 0, 1, 0xc1, 0, 0, 0, 1, 0xe1, 0, 0, 2, 0xe2, 0})
	feed(packet(0, append([]byte{0}, pat...), true, 0))
	feed(packet(256, append([]byte{0}, testPMT(1, 288, 257, 0)...), true, 0))
	feed(packet(512, append([]byte{0}, testPMT(2, 288, 513, 0)...), true, 0))
	feed(testECM(288, 0))
	sharedAssembler := d.sections[288]
	sharedKeys := d.ecms[288]
	if sharedKeys == nil {
		t.Fatal("shared ECM did not produce control words")
	}
	feed(packet(256, append([]byte{0}, testPMT(1, 289, 257, 1)...), true, 1))
	if d.sections[288] != sharedAssembler || d.ecms[288] != sharedKeys {
		t.Fatal("active shared ECM was retired")
	}
	p := packet(513, make([]byte, 184), false, 0)
	p[3] |= 0x80
	if ready, err := d.descramble(p); err != nil || !ready {
		t.Fatalf("remaining shared service lost keys: %v", err)
	}
	feed(packet(512, append([]byte{0}, testPMT(2, 290, 513, 1)...), true, 1))
	if d.sections[288] != nil || d.ecms[288] != nil {
		t.Fatal("last unreferenced ECM remained active")
	}
	for _, cipher := range []*multi2{sharedKeys.odd, sharedKeys.even} {
		if cipher.work != [8]uint32{} {
			t.Fatal("retired expanded keys were retained")
		}
	}
	feed(packet(256, append([]byte{0}, testPMT(1, 288, 257, 2)...), true, 2))
	p = packet(257, make([]byte, 184), false, 0)
	p[3] |= 0x80
	if ready, err := d.descramble(p); err != nil || ready {
		t.Fatalf("reused ECM accepted stale control words: ready=%v err=%v", ready, err)
	}
	feed(testECM(288, 0))
	if c.calls != 2 {
		t.Fatalf("reused ECM did not request current keys: %d", c.calls)
	}
	if ready, err := d.descramble(p); err != nil || !ready {
		t.Fatalf("reused ECM unavailable after new response: %v", err)
	}
}
