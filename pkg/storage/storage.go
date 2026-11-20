package storage

import "fmt"

// ErrorNotFound is returned when a requested configuration key is not set.
var ErrorNotFound = fmt.Errorf("not found")

type storage interface {
	Set(key string, value string) error
	SetDocument(key string, value any) error
	Get(key string) (map[string]any, error)
	Unset(key string) error
}
