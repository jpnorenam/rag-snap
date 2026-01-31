package engine

import (
	"testing"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/pkg/engines"
	"github.com/jpnorenam/rag-snap/pkg/hardware_info"
	"github.com/jpnorenam/rag-snap/pkg/selector"
	"github.com/jpnorenam/rag-snap/pkg/storage"
)

func TestList(t *testing.T) {
	cache := storage.NewMockCache()
	err := cache.SetActiveEngine("engine-name")
	if err != nil {
		t.Fatalf("Error setting active engine name: %v", err)
	}

	allEngines, err := engines.LoadManifests("../../../test_data/engines")
	if err != nil {
		t.Fatalf("error loading engines: %v", err)
	}

	hardwareInfo, err := hardware_info.GetFromRawData(t, "xps13-7390", true, "../../../test_data")
	if err != nil {
		t.Fatalf("error getting hardware info: %v", err)
	}

	scoredEngines, err := selector.ScoreEngines(hardwareInfo, allEngines)
	if err != nil {
		t.Fatalf("error scoring engines: %v", err)
	}

	// cmd.printEnginesTable needs to call `cmd.Cache.GetActiveEngine()` to get the current active engine
	// We therefore need to pass in the cache as context to `cmd`
	ctx := &common.Context{
		EnginesDir: "",
		Cache:      cache,
		Config:     nil,
	}
	cmd := listCommand{Context: ctx}

	err = cmd.printEnginesTable(scoredEngines)
	if err != nil {
		t.Fatal(err)
	}
}
