package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/internal/awssetup"
	"github.com/spf13/cobra"
)

// PrepareScriptCommand groups the commands that export preconfigured setup
// assets. A subcommand only writes files; the exported host script does the
// provisioning outside snap confinement.
func PrepareScriptCommand(_ *common.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "prepare-script",
		Short:   "Export setup scripts and templates",
		GroupID: groupID,
	}
	cmd.AddCommand(prepareScriptAWS())
	return cmd
}

// prepareScriptAWS exports the AWS setup assets.
func prepareScriptAWS() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "aws --output <directory>",
		Short: "Export the AWS setup script, guest bootstrap and Terraform templates",
		Long: "Write opensearch-on-aws.sh, bootstrap-opensearch.sh and Terraform templates into a directory you own.\n" +
			"Run './opensearch-on-aws.sh setup' from that directory (outside the snap) to provision a dedicated\n" +
			"OpenSearch instance on AWS and configure rag-cli; './opensearch-on-aws.sh destroy' removes it.\n" +
			"Re-running this command refreshes the scripts and keeps saved answers, secrets, keys and state.\n" +
			"Only AWS is supported today.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if os.Geteuid() == 0 {
				return errors.New("run this command as your normal user, without sudo")
			}
			dir, err := filepath.Abs(output)
			if err != nil {
				return err
			}
			if err := checkNotSnapTmp(dir); err != nil {
				return err
			}
			if err := awssetup.Write(dir, os.Getenv("SNAP_INSTANCE_NAME"), os.Getenv("SNAP_USER_COMMON")); err != nil {
				return err
			}
			fmt.Printf("Exported the AWS setup assets to %s\nNext: cd %s && ./opensearch-on-aws.sh setup\n", dir, dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "", "deployment directory to write (created if missing)")
	if err := cmd.MarkFlagRequired("output"); err != nil {
		panic(err)
	}
	return cmd
}

// checkNotSnapTmp rejects /tmp and /var/tmp: inside the snap they are private
// directories the unconfined host script cannot see.
func checkNotSnapTmp(dir string) error {
	for _, p := range []string{"/tmp", "/var/tmp"} {
		if dir == p || strings.HasPrefix(dir, p+"/") {
			return fmt.Errorf("%s is private to the snap and not the host's %s; choose a non-hidden directory under your home, e.g. ~/rag-aws", dir, p)
		}
	}
	return nil
}
