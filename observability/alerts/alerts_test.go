package alerts

import (
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewAlerting_EnvDisabled(t *testing.T) {
	_ = os.Setenv("ALERTING_ENABLED", "false")
	defer func() { _ = os.Unsetenv("ALERTING_ENABLED") }()

	reg := prometheus.NewRegistry()
	a := New(reg, "svc_test")

	// enabled should be 0
	if v := testutil.ToFloat64(a.enabled.WithLabelValues("svc_test")); v != 0 {
		t.Fatalf("expected enabled gauge 0 when env disabled, got %v", v)
	}

	// up should default to 1
	if v := testutil.ToFloat64(a.up.WithLabelValues("svc_test")); v != 1 {
		t.Fatalf("expected up gauge 1 by default, got %v", v)
	}
}

func TestSetEnabledAndUp(t *testing.T) {
	reg := prometheus.NewRegistry()
	a := New(reg, "svc2")

	a.SetEnabled(false)
	if v := testutil.ToFloat64(a.enabled.WithLabelValues("svc2")); v != 0 {
		t.Fatalf("expected enabled 0 after SetEnabled(false), got %v", v)
	}

	a.SetUp(false)
	if v := testutil.ToFloat64(a.up.WithLabelValues("svc2")); v != 0 {
		t.Fatalf("expected up 0 after SetUp(false), got %v", v)
	}

	a.SetEnabled(true)
	if v := testutil.ToFloat64(a.enabled.WithLabelValues("svc2")); v != 1 {
		t.Fatalf("expected enabled 1 after SetEnabled(true), got %v", v)
	}
}
