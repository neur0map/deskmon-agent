package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

const (
	DefaultPort        = 7654
	DefaultBind        = "0.0.0.0"
	DefaultConfigPath  = "/etc/deskmon/config.yaml"
	DefaultDockerSock  = "/var/run/docker.sock"
	DefaultSampleInterval = 1 // seconds
)

type Config struct {
	Port      int    `yaml:"port"`
	AuthToken string `yaml:"auth_token"`
	Bind      string `yaml:"bind"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Port: DefaultPort,
		Bind: DefaultBind,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Bind == "" {
		cfg.Bind = DefaultBind
	}

	return cfg, nil
}
