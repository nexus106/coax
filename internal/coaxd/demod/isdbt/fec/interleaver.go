package fec

// ByteDeinterleaver reverses STD-B31's twelve branches: receiver branch i
// delays 17*(11-i) bytes on that branch. With the transmitter this gives a
// uniform delay of 12*17*11 = 2244 stream bytes (11 RS packets). Push emits one
// byte per input byte, including zero-filled startup history; discard the first
// 2244 bytes before treating output as recovered transmitted data.
// Phase zero is the byte AFTER a TS synchronization byte (the sync uses branch
// eleven). Frame/byte synchronization and phase acquisition belong to the caller.
type ByteDeinterleaver struct {
	memory [12][187]byte
	cursor [12]int
	phase  int
}

func NewByteDeinterleaver() *ByteDeinterleaver { return &ByteDeinterleaver{} }

// Reset clears history and selects branch zero.
func (d *ByteDeinterleaver) Reset() { *d = ByteDeinterleaver{} }

// ResetPhase clears history and selects a branch, modulo twelve. This permits
// frame alignment when earlier decoder stages introduce a known byte offset.
func (d *ByteDeinterleaver) ResetPhase(phase int) { d.Reset(); d.phase = ((phase % 12) + 12) % 12 }
func (d *ByteDeinterleaver) Push(dst, src []byte) []byte {
	for _, value := range src {
		branch := d.phase
		length := 17 * (11 - branch)
		if length > 0 {
			pos := d.cursor[branch]
			old := d.memory[branch][pos]
			d.memory[branch][pos] = value
			value = old
			pos++
			if pos == length {
				pos = 0
			}
			d.cursor[branch] = pos
		}
		dst = append(dst, value)
		d.phase++
		if d.phase == 12 {
			d.phase = 0
		}
	}
	return dst
}
