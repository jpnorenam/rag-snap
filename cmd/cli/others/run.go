package others

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

type runCommand struct {
	*common.Context

	// flags
	waitForComponentsFlag bool
}

func RunCommand(ctx *common.Context) *cobra.Command {
	var cmd runCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:               "run <path>",
		Short:             "Run a subprocess",
		Hidden:            true,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE:              cmd.run,
	}

	// flags
	cobraCmd.Flags().BoolVar(&cmd.waitForComponentsFlag, "wait-for-components", false, "wait for engine components to be installed before running")

	return cobraCmd
}

func (cmd *runCommand) run(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("unexpected number of arguments, expected 1 got %d", len(args))
	}

	path := args[0]

	execCmd := exec.Command(path)
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	return execCmd.Run()
}
