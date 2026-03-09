package setting

import (
	"github.com/gurkankaymak/hocon"
)

type Config = hocon.Config

func LoadConfig(path string) (*hocon.Config, error) {
	if path == "" {
		path = "config/application.conf"
	}

	cfg, err := hocon.ParseResource(path)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}