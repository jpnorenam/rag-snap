package storage

import (
	"encoding/json"
	"strings"

	"github.com/canonical/go-snapctl"
)

// SnapctlStorage is the storage backend that keeps configuration in the snap's
// settings through snapctl.
type SnapctlStorage struct{}

// NewSnapctlStorage returns a snapctl-backed storage.
func NewSnapctlStorage() *SnapctlStorage {
	return &SnapctlStorage{}
}

// Set stores value under key as a plain string.
func (s *SnapctlStorage) Set(key, value string) error {
	return snapctl.Set(key, value).Run()
}

// SetDocument stores value under key as a JSON document.
func (s *SnapctlStorage) SetDocument(key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return snapctl.Set(key, string(b)).Document().Run()
}

// Get returns the value under key: a JSON object is decoded into a map, and a
// primitive value is returned as {key: value}. It returns ErrorNotFound when
// the key is unset.
func (s *SnapctlStorage) Get(key string) (map[string]any, error) {
	valJSON, err := snapctl.Get(key).Run()
	if err != nil {
		return nil, err
	}
	if valJSON == "" {
		return nil, ErrorNotFound
	}

	var valMap map[string]any
	if strings.HasPrefix(valJSON, "{") && strings.HasSuffix(valJSON, "}") {
		// Object value, parse as JSON
		err = json.Unmarshal([]byte(valJSON), &valMap)
		if err != nil {
			return nil, err
		}
	} else {
		// Primitive value, return as-is
		valMap = map[string]any{
			key: valJSON,
		}
	}

	return valMap, nil
}

// Unset removes key.
func (s *SnapctlStorage) Unset(key string) error {
	err := snapctl.Unset(key).Run()
	if err != nil {
		return err
	}
	return nil
}
