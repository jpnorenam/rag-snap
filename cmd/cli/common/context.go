package common

import "github.com/jpnorenam/rag-snap/pkg/storage"

type Context struct {
	Verbose bool
	Debug   bool
	Config  storage.Config
}
