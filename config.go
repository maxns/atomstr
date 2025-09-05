package main

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Hooks configuration
type HooksConfig struct {
	Hooks HookStages `yaml:"hooks"`
}

type HookStages struct {
	PrePostNostrPublish    []NamedHook `yaml:"prePostNostrPublish"`
	PreNostrProfilePublish []NamedHook `yaml:"preNostrProfilePublish"`
}

// NamedHook is a generic hook descriptor with a type and name.
// Currently supported type: "restEnrich"
type NamedHook struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`

	// REST hook fields
	URL            string            `yaml:"url"`
	Method         string            `yaml:"method"`
	Headers        map[string]string `yaml:"headers"`
	ComposeRequest string            `yaml:"composeRequestFunc"`
	ParseResponse  string            `yaml:"parseResponseFunc"`

	// EnrichWithTags fields
	SuggestTagsURL string `yaml:"suggestTagsUrl"`
}

func loadHooksConfig(path string) (*HooksConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	log.Printf("[DEBUG] Loaded hooks config from %s", path)
	cfg := &HooksConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	log.Printf("[DEBUG] Parsed hooks config: %+v", cfg.Hooks)
	return cfg, nil
}

func findDefaultHooksConfig() (string, error) {
	// prefer HOOKS_CONFIG_PATH, otherwise look for hooks.yaml in CWD
	if p := os.Getenv("HOOKS_CONFIG_PATH"); p != "" {
		return p, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	path := filepath.Join(wd, "hooks.yaml")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if errors.Is(err, fs.ErrNotExist) {
		return "", fs.ErrNotExist
	}
	return "", err
}
