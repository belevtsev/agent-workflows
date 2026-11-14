package gateway

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config is read once at start; there is no reload.
type Config struct {
	Tenants map[string]Tenant `yaml:"tenants"`
}

type Tenant struct {
	Name string `yaml:"name"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
