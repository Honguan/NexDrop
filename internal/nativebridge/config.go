package nativebridge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func LoadConfig() (Config, error) {
	path := os.Getenv("NEXDROP_BRIDGE_CONFIG")
	if path == "" {
		var root string
		if runtime.GOOS == "windows" {
			root = os.Getenv("LOCALAPPDATA")
		} else if root = os.Getenv("XDG_CONFIG_HOME"); root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return Config{}, err
			}
			root = filepath.Join(home, ".config")
		}
		path = filepath.Join(root, "NexDrop", "bridge.json")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	if json.Unmarshal(content, &config) != nil {
		return Config{}, ErrInvalidMessage
	}
	if _, err := NewClient(config.URL, config.Token); err != nil {
		return Config{}, errors.Join(ErrInvalidMessage, err)
	}
	return config, nil
}
