package basic

import (
	"context"
	"fmt"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/internal/apiclient"
)

// daemonClient returns a connected API client when a ragd daemon is running and
// the caller is trusted, or nil otherwise. Commands prefer the daemon (which
// owns the backend clients and secrets) and fall back to constructing backend
// clients directly when it returns nil. Detection is skipped in --debug mode,
// where the file-based config implies offline/inspection use.
func daemonClient(ctx *common.Context) *apiclient.Client {
	if ctx.Debug {
		return nil
	}
	return apiclient.Detect()
}

// waitWithProgress drives an async operation to completion, rendering a spinner
// labelled with the operation's progress metadata. done/total field names vary
// per operation (e.g. sources_done/sources_total, questions_done/questions_total);
// pass the pair to surface. An empty totalKey shows a static label.
func waitWithProgress(client *apiclient.Client, opURL, label, doneKey, totalKey string) (*apiclient.Operation, error) {
	update, stop := common.StartUpdatableSpinner(label)
	defer stop()

	return client.WaitForOperation(context.Background(), opURL, apiclient.WaitOptions{
		OnProgress: func(op *apiclient.Operation) {
			if totalKey == "" {
				return
			}
			if total := op.MetadataInt(totalKey); total > 0 {
				update(fmt.Sprintf("%s (%d/%d)", label, op.MetadataInt(doneKey), total))
			}
		},
	})
}
