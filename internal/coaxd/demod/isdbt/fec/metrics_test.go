package fec

import (
	"math/rand"
	"testing"
)

// The accelerated kernel must preserve every survivor decision, including
// ties, not merely recover a noiseless test stream.
func TestMetricKernel(t *testing.T) {
	rng := rand.New(rand.NewSource(1381))
	for n := 0; n < 1000; n++ {
		var current, next, want [64]int32
		var costs [32]int32
		for i := range current {
			current[i] = int32(rng.Intn(10001))
		}
		for i := range costs {
			costs[i] = int32(rng.Intn(3))
		}
		var decisions uint64
		for p := 0; p < 32; p++ {
			a, b := current[p], current[p+32]
			c := costs[p]
			d := 2 - c
			want[p*2] = a + c
			if b+d < want[p*2] {
				want[p*2] = b + d
				decisions |= 1 << uint(p*2)
			}
			want[p*2+1] = a + d
			if b+c < want[p*2+1] {
				want[p*2+1] = b + c
				decisions |= 1 << uint(p*2+1)
			}
		}
		if got := updateMetrics(&current, &next, &costs, 2); got != decisions || next != want {
			t.Fatalf("kernel iteration%d decisions %016x want%016x metrics equal%t", n, got, decisions, next == want)
		}
	}
}
