package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	WebDAVURL      string `yaml:"webdav_url"`
	WebDAVUsername string `yaml:"webdav_username"`
	WebDAVPassword string `yaml:"webdav_password"`
	MasterKey      string `yaml:"master_key"`
	JWTSecret      string `yaml:"jwt_secret"`
	DBPath         string `yaml:"db_path"`
	Port           string `yaml:"port"`
}

// configFile is the on-disk format: secrets nested under a "secrets:" key
// so the same file can be passed directly to helm with -f .secrets.yaml.
type configFile struct {
	Secrets Config `yaml:"secrets"`
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read secrets: %w", err)
	}
	var cf configFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parse secrets: %w", err)
	}
	cfg := cf.Secrets
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "./dl.db"
	}
	if cfg.WebDAVURL == "" {
		return nil, fmt.Errorf("webdav_url is required")
	}
	if cfg.MasterKey == "" {
		return nil, fmt.Errorf("master_key is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("jwt_secret is required")
	}
	return &cfg, nil
}
