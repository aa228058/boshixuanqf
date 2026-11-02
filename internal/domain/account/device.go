package account

import (
	"github.com/google/uuid"

	device "github.com/mtgo-labs/device-manager"
	"github.com/mtgo-labs/mtgo/telegram"
)

func generateDeviceConfig(sessionName, deviceType string) telegram.DeviceConfig {
	cfg := &telegram.Config{}
	switch deviceType {
	case "desktop":
		device.Desktop.Generate(sessionName).Apply(cfg)
		cfg.Device.AppVersion = "7.0.9"
	case "macos":
		device.MacOS.Generate(sessionName).Apply(cfg)
		cfg.Device.AppVersion = "7.0.9"
	case "ios":
		fallthrough
	default:
		device.IOS.Generate(sessionName).Apply(cfg)
	}
	return cfg.Device
}

// pickDeviceType selects device type for account.
// All accounts use macOS for consistent behavior and better compatibility.
func pickDeviceType(accountID uuid.UUID) string {
	return "macos"
}

// deviceProfileToConfig converts a saved device profile map to telegram.DeviceConfig.
// Reads device_type from profile, falls back to deterministic selection from sessionName.

func deviceProfileToConfig(profile map[string]any, sessionName string) telegram.DeviceConfig {
	if sessionName == "" {
		return telegram.DeviceConfig{}
	}
	// Restore saved device config from DB to keep consistent identity
	cfg := telegram.DeviceConfig{}
	if model, ok := profile["model"].(string); ok && model != "" {
		cfg.DeviceModel = model
	}
	if system, ok := profile["system"].(string); ok && system != "" {
		cfg.SystemVersion = system
	}
	if app, ok := profile["app"].(string); ok && app != "" {
		cfg.AppVersion = app
	}
	if langPack, ok := profile["lang_pack"].(string); ok && langPack != "" {
		cfg.LangPack = langPack
	}
	if langCode, ok := profile["lang_code"].(string); ok && langCode != "" {
		cfg.LangCode = langCode
	}
	if sysLangCode, ok := profile["system_lang_code"].(string); ok && sysLangCode != "" {
		cfg.SystemLangCode = sysLangCode
	}
	// If any critical field is missing, generate new config
	if cfg.DeviceModel == "" || cfg.SystemVersion == "" || cfg.AppVersion == "" {
		deviceType, _ := profile["device_type"].(string)
		if deviceType == "" {
			deviceType = "macos"
		}
		return generateDeviceConfig(sessionName, deviceType)
	}
	return cfg
}

// deviceConfigToProfile converts telegram.DeviceConfig to a map for DB storage.

func deviceConfigToProfile(d telegram.DeviceConfig, deviceType string) map[string]any {
	return map[string]any{
		"model":            d.DeviceModel,
		"system":           d.SystemVersion,
		"app":              d.AppVersion,
		"lang_pack":        d.LangPack,
		"lang_code":        d.LangCode,
		"system_lang_code": d.SystemLangCode,
		"platform":         deviceType,
		"device_type":      deviceType,
	}
}

// StartSessionBackup starts a background goroutine to periodically backup session files.
