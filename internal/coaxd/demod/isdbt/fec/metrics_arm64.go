//go:build arm64 && !purego

package fec

// updateMetrics performs eight four-butterfly NEON groups. It uses the same
// signed metrics and strict-less-than tie rule as the portable implementation.
//
//go:noescape
func updateMetrics(current, next *[64]int32, costs *[32]int32, total int32) uint64
