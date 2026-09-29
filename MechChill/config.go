package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows/registry"
)

const (
	ConfigFileName = "config.json"
	AppName        = "MechChill"
	LegacyAppName  = "MechSound"
	RunRegistryKey = `Software\Microsoft\Windows\CurrentVersion\Run`
)

type Config struct {
	Volume         int    `json:"volume"`          // 0 to 100
	Enabled        bool   `json:"enabled"`         // true / false
	RandomSounds   bool   `json:"random_sounds"`   // true / false
	SoundPack      string `json:"sound_pack"`      // "Default" or custom folder
	StartupEnabled bool   `json:"startup_enabled"` // run at Windows startup
	AllowRepeat    bool   `json:"allow_repeat"`    // repeat sound when holding key down
	ReleaseSound   bool   `json:"release_sound"`   // play release sound on key up (bas-çek)
}

var (
	appConfig   Config
	configMutex sync.RWMutex
)

// DefaultConfig returns reasonable default settings
func DefaultConfig() Config {
	return Config{
		Volume:         50,
		Enabled:        true,
		RandomSounds:   true,
		SoundPack:      "CherryMX-Blue",
		StartupEnabled: false,
		AllowRepeat:    false,
		ReleaseSound:   true,
	}
}

// LoadConfig loads settings from config.json or creates default if missing
func LoadConfig() Config {
	configMutex.Lock()
	defer configMutex.Unlock()

	cfg := DefaultConfig()

	data, err := os.ReadFile(ConfigFileName)
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			fmt.Printf("[Config] Error parsing %s, using defaults: %v\n", ConfigFileName, err)
			cfg = DefaultConfig()
		}
	} else if os.IsNotExist(err) {
		// Create default config file
		saveConfigInternal(cfg)
	}

	// Clamp volume between 0 and 100
	if cfg.Volume < 0 {
		cfg.Volume = 0
	} else if cfg.Volume > 100 {
		cfg.Volume = 100
	}

	// Sync startup status with Windows Registry
	cfg.StartupEnabled = checkStartupRegistry()

	appConfig = cfg
	return cfg
}

// GetConfig returns a thread-safe copy of current config
func GetConfig() Config {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return appConfig
}

// UpdateConfig updates and saves config
func UpdateConfig(fn func(c *Config)) Config {
	configMutex.Lock()
	defer configMutex.Unlock()

	fn(&appConfig)

	// Clamp volume
	if appConfig.Volume < 0 {
		appConfig.Volume = 0
	} else if appConfig.Volume > 100 {
		appConfig.Volume = 100
	}

	saveConfigInternal(appConfig)
	return appConfig
}

func saveConfigInternal(cfg Config) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Printf("[Config] Error marshaling config: %v\n", err)
		return
	}
	_ = os.WriteFile(ConfigFileName, data, 0644)
}

// SaveConfig explicitly writes the current config to disk
func SaveConfig() {
	configMutex.RLock()
	cfg := appConfig
	configMutex.RUnlock()
	saveConfigInternal(cfg)
}

// SetStartupRegistry configures MechChill to run at Windows login
func SetStartupRegistry(enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, RunRegistryKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("failed to open registry key: %w", err)
	}
	defer k.Close()

	// Clean up legacy MechSound entry if present
	_ = k.DeleteValue(LegacyAppName)

	if enable {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to determine executable path: %w", err)
		}
		exePath, err = filepath.Abs(exePath)
		if err != nil {
			return err
		}
		// Wrap in quotes
		val := fmt.Sprintf("\"%s\"", exePath)
		if err := k.SetStringValue(AppName, val); err != nil {
			return fmt.Errorf("failed to set registry value: %w", err)
		}
	} else {
		_ = k.DeleteValue(AppName)
	}

	UpdateConfig(func(c *Config) {
		c.StartupEnabled = enable
	})
	return nil
}

func checkStartupRegistry() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, RunRegistryKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	if _, _, err = k.GetStringValue(AppName); err == nil {
		return true
	}
	_, _, err = k.GetStringValue(LegacyAppName)
	return err == nil
}
