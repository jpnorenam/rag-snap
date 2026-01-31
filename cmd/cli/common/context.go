package common

import "github.com/jpnorenam/rag-snap/pkg/storage"

type Context struct {
	EnginesDir string
	Verbose    bool
	Cache      storage.Cache
	Config     storage.Config
}
