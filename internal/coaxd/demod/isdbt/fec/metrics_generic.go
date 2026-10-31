//go:build !arm64 || purego

package fec

func updateMetrics(current, next *[64]int32, costs *[32]int32, total int32) uint64 {
	return updateMetricsGo(current, next, costs, total)
}
