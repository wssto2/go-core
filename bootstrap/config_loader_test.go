package bootstrap

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type testConfig struct {
	AppName string        `env:"TEST_APP_NAME" validation:"required"`
	Port    int           `env:"TEST_PORT" validation:"required"`
	Debug   bool          `env:"TEST_DEBUG"`
	Timeout time.Duration `env:"TEST_TIMEOUT"`
	Nested  struct {
		Key string `env:"TEST_NESTED_KEY" validation:"required"`
	}
}

func TestLoadConfig(t *testing.T) {
	os.Setenv("TEST_APP_NAME", "MyApp")
	os.Setenv("TEST_PORT", "9090")
	os.Setenv("TEST_DEBUG", "true")
	os.Setenv("TEST_TIMEOUT", "5s")
	os.Setenv("TEST_NESTED_KEY", "secret")
	defer func() {
		os.Unsetenv("TEST_APP_NAME")
		os.Unsetenv("TEST_PORT")
		os.Unsetenv("TEST_DEBUG")
		os.Unsetenv("TEST_TIMEOUT")
		os.Unsetenv("TEST_NESTED_KEY")
	}()

	cfg := &testConfig{}
	err := LoadConfig(cfg)

	assert.NoError(t, err)
	assert.Equal(t, "MyApp", cfg.AppName)
	assert.Equal(t, 9090, cfg.Port)
	assert.True(t, cfg.Debug)
	assert.Equal(t, 5*time.Second, cfg.Timeout)
	assert.Equal(t, "secret", cfg.Nested.Key)
}

func TestLoadConfig_FloatAndUintFields(t *testing.T) {
	type TestCfg struct {
		Score float64 `env:"TEST_SCORE"`
		Count uint    `env:"TEST_COUNT"`
		Rate  float32 `env:"TEST_RATE"`
		Limit uint32  `env:"TEST_LIMIT"`
	}

	t.Setenv("TEST_SCORE", "3.14")
	t.Setenv("TEST_COUNT", "42")
	t.Setenv("TEST_RATE", "1.5")
	t.Setenv("TEST_LIMIT", "100")

	var cfg TestCfg
	if err := LoadConfig(&cfg); err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if cfg.Score != 3.14 {
		t.Errorf("Score: got %v, want 3.14", cfg.Score)
	}
	if cfg.Count != 42 {
		t.Errorf("Count: got %v, want 42", cfg.Count)
	}
	if cfg.Rate != 1.5 {
		t.Errorf("Rate: got %v, want 1.5", cfg.Rate)
	}
	if cfg.Limit != 100 {
		t.Errorf("Limit: got %v, want 100", cfg.Limit)
	}
}

func TestLoadConfig_ValidationError(t *testing.T) {
	os.Setenv("TEST_PORT", "9090")
	defer os.Unsetenv("TEST_PORT")

	cfg := &testConfig{}
	err := LoadConfig(cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}
