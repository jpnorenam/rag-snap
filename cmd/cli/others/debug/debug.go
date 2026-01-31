package debug

import (
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

func DebugCommand(ctx *common.Context) *cobra.Command {
	debugCmd := &cobra.Command{
		Use:    "debug",
		Long:   "Developer/debugging commands",
		Hidden: true,
	}

	debugCmd.AddCommand(
		ValidateCommand(ctx),
		SelectCommand(ctx),
		ChatCommand(ctx),
	)

	return debugCmd
}
