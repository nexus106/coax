package fec

import (
	"fmt"
	"math/bits"
)

// Trace back 96 lookahead bits and a 32-bit output block through the K=7
// trellis. A bounded traceback can still make mistakes on noisy input.
const traceback = 128
const outputBits = 32

var punctures = [...]string{"11", "1101", "110110", "1101100110", "11010101100110"}

// Viterbi is a streaming, hard-decision 64-state decoder. Input values must be
// zero or one and start at the first X/Y puncturing phase of a frame. Decode
// appends MSB-first information bytes. It first emits four bytes after 128
// information bits, then four bytes every 32 bits (96 bits retained as lookahead).
// The caller must supply later symbols to recover trailing information; there
// is no implicit end-of-stream flush. Instances are independent, but concurrent
// calls on the same instance require caller synchronization.
type Viterbi struct {
	pattern string
	phase   int
	pair    [2]byte
	mask    byte
	metrics [2][64]int32
	history [traceback]uint64
	steps   uint64
}

// NewViterbi accepts rates 0..4 for 1/2, 2/3, 3/4, 5/6 and 7/8.
func NewViterbi(rate int) (*Viterbi, error) {
	if rate < 0 || rate >= len(punctures) {
		return nil, fmt.Errorf("fec: invalid convolutional rate %d", rate)
	}
	v := &Viterbi{pattern: punctures[rate]}
	v.Reset()
	return v, nil
}

// Reset starts a zero-state encoder trellis and the first puncturing phase.
func (v *Viterbi) Reset() {
	pattern := v.pattern
	*v = Viterbi{pattern: pattern}
	for s := 1; s < 64; s++ {
		v.metrics[0][s] = 10000
	}
}

// Decode preserves puncturing position, including a half-received X/Y pair,
// trellis survivors and output alignment across arbitrarily sized calls.
func (v *Viterbi) Decode(dst, symbols []byte) []byte {
	for len(symbols) > 0 {
		slot := v.phase & 1
		if v.pattern[v.phase] == '1' {
			v.pair[slot] = symbols[0] & 1
			v.mask |= 1 << slot
			symbols = symbols[1:]
		}
		v.phase = (v.phase + 1) % len(v.pattern)
		if slot == 1 {
			dst = v.step(dst)
			v.mask = 0
		}
	}
	// A trailing puncture has no input symbol; finish its mother-code pair now.
	// Do not advance an entire all-punctured pair (none exist in STD-B31).
	if v.phase&1 == 1 && v.pattern[v.phase] == '0' {
		v.phase = (v.phase + 1) % len(v.pattern)
		dst = v.step(dst)
		v.mask = 0
	}
	return dst
}

// With the current input stored as register bit zero, Fig.3-10's taps are
// 0,1,2,3,6 for X and 0,2,3,5,6 for Y. Thus 171/133 octal as printed in the
// standard must be BIT REVERSED to 117/155 octal for this register orientation.
var trellisOutputs = func() (out [128]byte) {
	for reg := range out {
		out[reg] = byte(bits.OnesCount(uint(reg&0o117))&1) | byte(bits.OnesCount(uint(reg&0o155))&1)<<1
	}
	return
}()

func (v *Viterbi) step(dst []byte) []byte {
	received := v.pair[0] | v.pair[1]<<1
	var costs [4]int32
	for i := range costs {
		costs[i] = int32(bits.OnesCount8((byte(i) ^ received) & v.mask))
	}
	current := &v.metrics[v.steps&1]
	next := &v.metrics[(v.steps+1)&1]
	var decisions uint64
	// Each butterfly has two predecessors and two successors. Both the new
	// input and the oldest delay bit complement X and Y, so two costs serve
	// all four branches. Normalization is only needed once per 32 steps.
	for prev := 0; prev < 32; prev++ {
		state := prev * 2
		symbol := trellisOutputs[state]
		c0, c1 := costs[symbol], costs[symbol^3]
		a, b := current[prev], current[prev+32]
		lo, hi := a+c0, b+c1
		difference := hi - lo
		choose := uint32(difference) >> 31
		next[state] = min(lo, hi)
		decisions |= uint64(choose) << state
		lo, hi = a+c1, b+c0
		difference = hi - lo
		choose = uint32(difference) >> 31
		next[state+1] = min(lo, hi)
		decisions |= uint64(choose) << (state + 1)
	}
	v.history[v.steps%traceback] = decisions
	v.steps++
	if v.steps%outputBits != 0 {
		return dst
	}
	minimum := int32(1 << 30)
	best := 0
	for state, metric := range next {
		if metric < minimum {
			minimum = metric
			best = state
		}
	}
	for state := range next {
		next[state] -= minimum
	}
	if v.steps < traceback {
		return dst
	}
	var block [4]byte
	state := best
	for age := 0; age < traceback; age++ {
		if age >= traceback-outputBits {
			index := traceback - 1 - age
			block[index/8] |= byte(state&1) << uint(7-index%8)
		}
		decision := (v.history[(v.steps-1-uint64(age))%traceback] >> state) & 1
		state = (state >> 1) | int(decision)<<5
	}
	return append(dst, block[:]...)
}
