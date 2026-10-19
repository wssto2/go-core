package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func BenchmarkRetry_Success(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := Retry(ctx, 3, time.Microsecond, func(ctx context.Context) error { return nil }); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

func BenchmarkRetry_Failure(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Retry(ctx, 3, time.Microsecond, func(ctx context.Context) error { return errors.New("fail") })
	}
}
