package testdata

import "time"

//nolint:revive
type Config struct {
	Config struct {
		Active struct {
			Profiles string
		}
	}
	App struct {
		URL       string
		AutoStart bool
	}
	HTTPClientTimeout time.Duration
}

var Cfg Config
