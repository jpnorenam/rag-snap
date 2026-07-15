package config

import (
	"fmt"
	"slices"

	"github.com/jpnorenam/rag-snap/pkg/storage"
	"github.com/spf13/cobra"
)

const groupID = "config"

// deprecatedConfig lists configurations the user no longer sets. They are still
// consumed by the engines, so they are hidden from listings and rejected on write
// rather than deleted. Consumers outside this package go through IsDeprecated.
var deprecatedConfig = []string{
	"model",
	"model-name",
	"multimodel-projector",
	"server",
	"target-device",
	"http.base-path",
}

// IsDeprecated reports whether a config key is deprecated. The CLI hides these keys
// from `config get` and rejects them in `config set`; the daemon's config API applies
// the same rule, so both surfaces stay in step from one list.
func IsDeprecated(key string) bool {
	return slices.Contains(deprecatedConfig, key)
}

func Group(title string) *cobra.Group {
	return &cobra.Group{
		ID:    groupID,
		Title: title,
	}
}

// GetValue retrieves a single config value by key.
func GetValue(cfg storage.Config, key string) (any, error) {
	configMap, err := cfg.Get(key)
	if err != nil {
		return nil, fmt.Errorf("error getting %q: %w", key, err)
	}
	return configMap[key], nil
}

// GetString retrieves a single config value as a string.
// Non-string values (e.g. numeric ports from snapctl) are formatted with %v.
func GetString(cfg storage.Config, key string) (string, error) {
	val, err := GetValue(cfg, key)
	if err != nil {
		return "", err
	}
	if val == nil {
		return "", nil
	}
	if s, ok := val.(string); ok {
		return s, nil
	}
	return fmt.Sprintf("%v", val), nil
}
