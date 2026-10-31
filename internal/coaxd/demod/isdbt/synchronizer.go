// Copyright 2026 nexus106
// SPDX-License-Identifier: Apache-2.0 OR GPL-2.0-or-later

package isdbt

import "math"

// Symbol is valid only during the callback; Data is in logical segment order.
type Symbol struct {
	Data               []complex64
	Index, FrameSymbol int
	Parameters         *Parameters
	FrameEnd           bool
}
type SyncSnapshot struct {
	Parameters             *Parameters
	Mode, GuardDenominator int
	Symbols, TMCCFrames    uint64
	FrequencyOffset, CNR   float64
}

type Synchronizer struct {
	buffer                                   []complex64
	total                                    int64
	acquired                                 bool
	n, cp, period, prefix, index, frameIndex int
	mode, guard                              int
	offset                                   float64
	plan                                     *fft
	spectrum, channel, active, data          []complex64
	pilots                                   []float32
	reserved                                 []bool
	tmcc                                     []int
	previous                                 []complex64
	history                                  [204]byte
	historyAt, historyCount                  int
	snapshot                                 SyncSnapshot
}

func NewSynchronizer() *Synchronizer {
	return &Synchronizer{buffer: make([]complex64, 0, 262144), frameIndex: -1}
}
func (s *Synchronizer) Snapshot() SyncSnapshot { return s.snapshot }
func (s *Synchronizer) Reset() {
	s.buffer = s.buffer[:0]
	s.acquired = false
	s.total = 0
	s.historyAt = 0
	s.historyCount = 0
	s.frameIndex = -1
	s.snapshot = SyncSnapshot{}
	clear(s.previous)
}
func (s *Synchronizer) PushIQ(x []complex64, discontinuity bool, emit func(Symbol)) {
	if discontinuity {
		s.Reset()
	}
	s.buffer = append(s.buffer, x...)
	if !s.acquired {
		if len(s.buffer) < 131072 {
			return
		}
		a, ok := acquire(s.buffer[:min(len(s.buffer), 163840)])
		if !ok {
			drop := len(s.buffer) - 65536
			copy(s.buffer, s.buffer[drop:])
			s.buffer = s.buffer[:65536]
			s.total += int64(drop)
			return
		}
		s.mode = a.Mode
		s.guard = a.GuardDenominator
		s.n = 1 << (10 + s.mode)
		s.cp = s.n / s.guard
		s.period = s.n + s.cp
		s.prefix = a.PrefixStart
		s.offset = a.FractionalOffset
		s.plan, _ = newFFT(s.n)
		count := 13*108*(1<<(s.mode-1)) + 1
		s.spectrum = make([]complex64, s.n)
		s.active = make([]complex64, count)
		s.channel = make([]complex64, count)
		s.data = make([]complex64, 13*96*(1<<(s.mode-1)))
		s.pilots = pilotSigns(count)
		s.reserved = make([]bool, count)
		s.tmcc = nil
		for seg := 0; seg < 13; seg++ {
			for _, r := range tmccRows[s.mode] {
				k := 108*(1<<(s.mode-1))*seg + r[seg]
				s.tmcc = append(s.tmcc, k)
				s.reserved[k] = true
			}
			for _, r := range acRows[s.mode] {
				s.reserved[108*(1<<(s.mode-1))*seg+r[seg]] = true
			}
		}
		s.previous = make([]complex64, len(s.tmcc))
		s.acquired = true
		s.index = -1
		s.snapshot.Mode = s.mode
		s.snapshot.GuardDenominator = s.guard
	}
	for s.prefix+s.period+48 < len(s.buffer) {
		pos, cfo, coherence := trackPrefix(s.buffer, s.prefix, s.n, s.cp)
		if coherence < .4 {
			s.Reset()
			return
		}
		s.prefix = pos
		currentFraction := s.offset - math.Round(s.offset)
		difference := cfo - currentFraction
		difference -= math.Round(difference)
		s.offset += .05 * difference
		s.transform(s.prefix + s.cp - 8)
		shift, idx, quality := s.findPilots(s.index < 0)
		if quality < .25 {
			s.Reset()
			return
		}
		if s.index < 0 {
			s.offset += float64(shift)
			s.transform(s.prefix + s.cp - 8)
		}
		s.index = idx
		s.equalize()
		s.snapshot.Symbols++
		s.snapshot.FrequencyOffset = s.offset * 8126984 / float64(s.n)
		params, end := s.receiveTMCC()
		if s.frameIndex >= 0 {
			s.frameIndex = (s.frameIndex + 1) % 204
		}
		if end {
			s.frameIndex = 203
		}
		s.extractData()
		if emit != nil {
			emit(Symbol{Data: s.data, Index: s.index, FrameSymbol: s.frameIndex, Parameters: params, FrameEnd: end})
		}
		s.prefix += s.period
	}
	// Keep just enough history for the next symbol's timing search.
	if drop := s.prefix - 48; drop > 0 {
		copy(s.buffer, s.buffer[drop:])
		s.buffer = s.buffer[:len(s.buffer)-drop]
		s.prefix -= drop
		s.total += int64(drop)
	}
}
func pilotSigns(count int) []float32 {
	out := make([]float32, count)
	state := uint16(2047)
	for k := range out {
		out[k] = 1 - 2*float32(state&1)
		feedback := (state ^ (state >> 2)) & 1
		state = (state >> 1) | (feedback << 10)
	}
	return out
}

func trackPrefix(x []complex64, predicted, n, cp int) (int, float64, float64) {
	lo := max(0, predicted-24)
	hi := min(predicted+24, len(x)-n-cp-1)
	var sum complex128
	var power float64
	for j := 0; j < cp; j++ {
		a, b := x[lo+j], x[lo+j+n]
		sum += complex128(b * conjugate(a))
		power += float64(real(a)*real(a)+imag(a)*imag(a)+real(b)*real(b)+imag(b)*imag(b)) * .5
	}
	best := -math.MaxFloat64
	pos := predicted
	var cross complex128
	var coherence float64
	for i := lo; i <= hi; i++ {
		metric := math.Hypot(real(sum), imag(sum)) - .95*power
		if metric > best {
			best = metric
			pos = i
			cross = sum
			if power > 0 {
				coherence = math.Hypot(real(sum), imag(sum)) / power
			}
		}
		a, b, c, d := x[i], x[i+n], x[i+cp], x[i+n+cp]
		sum += complex128(d*conjugate(c) - b*conjugate(a))
		power += float64(real(c)*real(c)+imag(c)*imag(c)+real(d)*real(d)+imag(d)*imag(d)-real(a)*real(a)-imag(a)*imag(a)-real(b)*real(b)-imag(b)*imag(b)) * .5
	}
	return pos, math.Atan2(imag(cross), real(cross)) / (2 * math.Pi), coherence
}
func (s *Synchronizer) transform(start int) {
	phase := -2 * math.Pi * s.offset * float64(s.total+int64(start)) / float64(s.n)
	sn, cs := math.Sincos(math.Remainder(phase, 2*math.Pi))
	rotation := complex(float32(cs), float32(sn))
	sn, cs = math.Sincos(-2 * math.Pi * s.offset / float64(s.n))
	step := complex(float32(cs), float32(sn))
	for i := 0; i < s.n; i++ {
		s.spectrum[i] = s.buffer[start+i] * rotation
		rotation *= step
		if i&1023 == 1023 {
			mag := float32(math.Hypot(float64(real(rotation)), float64(imag(rotation))))
			rotation /= complex(mag, 0)
		}
	}
	s.plan.transform(s.spectrum)
}
func (s *Synchronizer) spectral(bin int) complex64 { return s.spectrum[(bin+s.n/2)%s.n] }
func (s *Synchronizer) findPilots(search bool) (int, int, float64) {
	left := s.n/2 - len(s.active)/2
	shiftMin, shiftMax := 0, 0
	if search {
		shiftMin = -64
		shiftMax = 64
	}
	best := -1.0
	bestShift, bestIndex := 0, 0
	for shift := shiftMin; shift <= shiftMax; shift++ {
		for index := 0; index < 4; index++ {
			var c complex128
			var p float64
			for k := 3*index + 12; k < len(s.active)-1; k += 12 {
				a := s.spectral(left+shift+k) * complex(s.pilots[k], 0)
				b := s.spectral(left+shift+k-12) * complex(s.pilots[k-12], 0)
				v := a * conjugate(b)
				c += complex128(v)
				p += math.Hypot(float64(real(v)), float64(imag(v)))
			}
			q := 0.0
			if p > 0 {
				q = math.Hypot(real(c), imag(c)) / p
			}
			if q > best {
				best = q
				bestShift = shift
				bestIndex = index
			}
		}
	}
	return bestShift, bestIndex, best
}
func (s *Synchronizer) equalize() {
	left := s.n/2 - len(s.active)/2
	first := 3 * s.index
	last := first
	for k := first; k < len(s.active)-1; k += 12 {
		s.channel[k] = s.spectral(left+k) * complex(s.pilots[k]*.75, 0)
		last = k
	}
	end := len(s.active) - 1
	s.channel[end] = s.spectral(left+end) * complex(s.pilots[end]*.75, 0)
	for k := 0; k < first; k++ {
		s.channel[k] = s.channel[first]
	}
	for a := first; a < last; a += 12 {
		for j := 1; j < 12; j++ {
			t := float32(j) / 12
			s.channel[a+j] = s.channel[a]*complex(1-t, 0) + s.channel[a+12]*complex(t, 0)
		}
	}
	for k := last + 1; k < end; k++ {
		t := float32(k-last) / float32(end-last)
		s.channel[k] = s.channel[last]*complex(1-t, 0) + s.channel[end]*complex(t, 0)
	}
	for k, h := range s.channel {
		den := real(h)*real(h) + imag(h)*imag(h)
		if den > 1e-16 {
			s.active[k] = s.spectral(left+k) * conjugate(h) / complex(den, 0)
		} else {
			s.active[k] = 0
		}
	}
}
func (s *Synchronizer) receiveTMCC() (*Parameters, bool) {
	vote := 0
	for j, k := range s.tmcc {
		v := s.active[k]
		if real(v*conjugate(s.previous[j])) >= 0 {
			vote++
		} else {
			vote--
		}
		s.previous[j] = v
	}
	bit := byte(0)
	if vote < 0 {
		bit = 1
	}
	s.history[s.historyAt] = bit
	s.historyAt = (s.historyAt + 1) % 204
	s.historyCount++
	if s.historyCount < 204 {
		return nil, false
	}
	word := 0
	for j := 1; j <= 16; j++ {
		word = word*2 + int(s.history[(s.historyAt+j)%204])
	}
	if word != 0x35ee && word != 0xca11 {
		return nil, false
	}
	frame := make([]byte, 204)
	for j := range frame {
		frame[j] = s.history[(s.historyAt+j)%204]
	}
	p, err := parseTMCC(frame, s.mode)
	if err != nil {
		return nil, false
	}
	p.GuardDenominator = s.guard
	s.snapshot.TMCCFrames++
	s.snapshot.Parameters = &p
	return &p, true
}
func (s *Synchronizer) extractData() {
	nc := 96 * (1 << (s.mode - 1))
	carriers := 108 * (1 << (s.mode - 1))
	for physical, logical := range physicalSegments {
		out := s.data[logical*nc : (logical+1)*nc]
		j := 0
		for k := physical * carriers; k < (physical+1)*carriers; k++ {
			if k%12 == 3*s.index || s.reserved[k] {
				continue
			}
			out[j] = s.active[k]
			j++
		}
	}
}
