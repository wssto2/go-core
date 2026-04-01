package resilience

import (
	"testing"
	"time"
)

func BenchmarkCircuitBreaker_State_Parallel(b *testing.B) {
	cb := NewCircuitBreaker(5, time.Second)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = cb.State()
		}
	})
}
