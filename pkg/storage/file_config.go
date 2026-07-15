package storage

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// fileConfig implements Config by reading key=value pairs from a flat file.
// It is read-only; Set, SetDocument, and Unset return errors.
type fileConfig struct {
	values map[string]any
}

// NewFileConfig reads the file at path and returns a Config backed by its contents.
// Each line must be in the format: key="value"
func NewFileConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening config file: %w", err)
	}
	defer f.Close()

	values := make(map[string]any)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, "\"")
		values[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	return &fileConfig{values: values}, nil
}

func (c *fileConfig) Get(key string) (map[string]any, error) {
	result := make(map[string]any)
	for k, v := range c.values {
		if k == key || strings.HasPrefix(k, key+".") {
			result[k] = v
		}
	}
	return result, nil
}

func (c *fileConfig) GetAll() (map[string]any, error) {
	result := make(map[string]any, len(c.values))
	for k, v := range c.values {
		result[k] = v
	}
	return result, nil
}

// GetAllFromLayer reports every value as a package value: the debug file is flat and
// read-only, so nothing in it can be a user override.
func (c *fileConfig) GetAllFromLayer(confType ConfigType) (map[string]any, error) {
	if confType != PackageConfig {
		return map[string]any{}, nil
	}
	return c.GetAll()
}

func (c *fileConfig) Set(key, value string, confType ConfigType) error {
	return fmt.Errorf("config is read-only in debug mode")
}

func (c *fileConfig) SetDocument(key string, value any, confType ConfigType) error {
	return fmt.Errorf("config is read-only in debug mode")
}

func (c *fileConfig) Unset(key string, confType ConfigType) error {
	return fmt.Errorf("config is read-only in debug mode")
}
