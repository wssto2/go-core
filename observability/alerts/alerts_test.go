package alerts

import (
	"os"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/wssto2/go-core/env"
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

func TestNewFromReadsAlertingEnabledThroughTheLookup(t *testing.T) {
	t.Setenv("ALERTING_ENABLED", "true") // the process says on, the lookup says off

	vars := env.New(func(k string) (string, bool) { return "off", k == "ALERTING_ENABLED" })
	a := NewFrom(prometheus.NewRegistry(), "svc_lookup", vars)

	if v := testutil.ToFloat64(a.enabled.WithLabelValues("svc_lookup")); v != 0 {
		t.Fatalf("expected enabled gauge 0, got %v", v)
	}

	b := NewFrom(prometheus.NewRegistry(), "svc_default", env.New(func(string) (string, bool) { return "", false }))
	if v := testutil.ToFloat64(b.enabled.WithLabelValues("svc_default")); v != 1 {
		t.Fatalf("expected enabled gauge 1 by default, got %v", v)
	}
}
