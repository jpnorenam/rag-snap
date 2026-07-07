package basic

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/canonical/go-snapctl/env"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

type uiCommand struct {
	*common.Context

	// noBrowser prints the URL instead of opening a browser.
	noBrowser bool
}

// UICommand launches the local browser UI. It contacts the ragd daemon over the
// trusted unix socket to discover the loopback listener's URL and localhost
// token, then opens the browser at the /ui/login token-handoff URL so the loaded
// UI can authenticate its API calls.
func UICommand(ctx *common.Context) *cobra.Command {
	var cmd uiCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:               "ui",
		Short:             "Open the local web UI",
		Long:              "Open the local browser UI for chatting with your knowledge bases.\n\nRequires the ragd daemon's loopback listener to be enabled (api.loopback.enabled).",
		GroupID:           groupID,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE:              cmd.run,
	}

	cobraCmd.Flags().BoolVar(&cmd.noBrowser, "no-browser", false, "print the UI URL instead of opening a browser")

	return cobraCmd
}

func (cmd *uiCommand) run(_ *cobra.Command, _ []string) error {
	client := daemonClient(cmd.Context)
	if client == nil {
		return fmt.Errorf("the ragd daemon is not running or you are not authorized to reach it.\n\nStart it with:\n  sudo snap start %s.ragd", snapName())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := client.ServerInfo(ctx)
	if err != nil {
		return fmt.Errorf("querying the daemon: %w", err)
	}

	// On this branch the loopback listener carries the UI; its state is exposed
	// as info.Config.Loopback (apiclient.LoopbackInfo), keyed by api.loopback.*.
	ui := info
	if !ui.Enabled {
		return cmd.reportDisabled()
	}
	if ui.URL == "" || ui.Token == "" {
		return fmt.Errorf("the daemon reported the loopback listener as enabled but did not return its URL/token; check the ragd service logs")
	}

	// The daemon returns the token over the peercred-authenticated socket, so
	// reaching this point means we are already authorized to use it. Build the
	// token-handoff URL: /ui/login?token=... sets a loopback cookie and
	// redirects into the SPA, keeping the token out of the SPA's JS.
	loginURL, err := buildLoginURL(ui.URL, ui.Token)
	if err != nil {
		return err
	}

	if cmd.noBrowser {
		fmt.Printf("Open the UI in your browser:\n\n  %s\n", loginURL)
		return nil
	}

	fmt.Printf("Opening the UI at %s/ui/\n", strings.TrimSuffix(ui.URL, "/"))
	fmt.Printf("If your browser does not open automatically, visit:\n\n  %s\n", loginURL)
	openBrowser(loginURL)
	return nil
}

// reportDisabled explains how to enable the loopback listener rather than
// failing silently (task 5.3).
func (cmd *uiCommand) reportDisabled() error {
	return fmt.Errorf(
		"the local UI is disabled.\n\nEnable it with:\n  sudo %s set api.loopback.enabled=true\n  sudo snap restart %s.ragd\n\nThen run `%s ui` again.",
		snapName(), snapName(), snapName(),
	)
}

// buildLoginURL builds the daemon's /ui/login token-handoff URL from the
// loopback base URL (like http://127.0.0.1:port, no trailing /ui/) and token.
func buildLoginURL(baseURL, token string) (string, error) {
	base := strings.TrimSuffix(baseURL, "/")
	u, err := url.Parse(base + "/ui/login")
	if err != nil {
		return "", fmt.Errorf("building UI login URL: %w", err)
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// snapName returns the snap instance name for user-facing instructions, falling
// back to "rag-cli" outside a snap.
func snapName() string {
	if name := env.SnapInstanceName(); name != "" {
		return name
	}
	return "rag-cli"
}

// openBrowser attempts to open url in the user's default browser.
func openBrowser(rawURL string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "linux":
		cmd, args = "xdg-open", []string{rawURL}
	case "darwin":
		cmd, args = "open", []string{rawURL}
	default:
		return
	}
	_ = exec.Command(cmd, args...).Start()
}
