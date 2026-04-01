package audit

import (
	"encoding/json"
	"testing"
)

var benchMaskInput = map[string]any{
	"user_id":  1,
	"email":    "user@example.com",
	"password": "hunter2",
	"name":     "Alice",
	"api_key":  "key-123-abc",
	"metadata": map[string]any{"plan": "pro", "active": true},
}

func BenchmarkMask_MapInput(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Mask(benchMaskInput)
	}
}

func BenchmarkMaskJSON_ByteInput(b *testing.B) {
	data, _ := json.Marshal(benchMaskInput)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MaskJSON(data)
	}
}

func BenchmarkMask_RawMessageInput(b *testing.B) {
	data, _ := json.Marshal(benchMaskInput)
	raw := json.RawMessage(data)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Mask(raw)
	}
}
