package common

import "github.com/jpnorenam/rag-snap/pkg/storage"

// Context carries the global flags and configuration store into every command.
type Context struct {
	Verbose bool
	Debug   bool
	Config  storage.Config
}
