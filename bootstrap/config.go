package bootstrap

type Config struct {
	AppName string
	Env     string // "production", "development"
	Port    int

	ReadTimeoutSec     int
	WriteTimeoutSec    int
	IdleTimeoutSec     int
	ShutdownTimeoutSec int
}

func DefaultConfig() Config {
	return Config{
		Port:               8080,
		Env:                "development",
		ReadTimeoutSec:     15,
		WriteTimeoutSec:    15,
		IdleTimeoutSec:     60,
		ShutdownTimeoutSec: 10,
	}
}
