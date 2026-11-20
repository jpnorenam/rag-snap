package common

import (
	"fmt"

	"github.com/canonical/go-snapctl/env"
)

// SuggestServerStartup suggests retrying once the server is ready.
func SuggestServerStartup() string {
	return "Try again when the server is ready."
}

// daemonServiceName returns the snap service name of the ragd daemon
// (e.g. "rag-cli.ragd"), falling back to a placeholder outside a snap.
func daemonServiceName() string {
	instanceName := env.SnapInstanceName()
	if instanceName == "" { // not a snap
		instanceName = "<snap-instance-name>"
	}
	return instanceName + ".ragd"
}

// SuggestServerLogs returns the command that shows the ragd daemon logs.
func SuggestServerLogs() string {
	return fmt.Sprintf("Run \"snap logs %s\" to see the server logs.", daemonServiceName())
}

// SuggestStartServer returns the command that starts the ragd daemon.
func SuggestStartServer() string {
	return fmt.Sprintf("Run \"sudo snap start %s\" to start the server.", daemonServiceName())
}

// SuggestSetModelID returns the command that writes a resolved model ID to the
// package configuration layer. Shown when the daemon could not persist it, or
// when a direct-mode init has nobody to persist it for the operator.
func SuggestSetModelID(key, value string) string {
	instanceName := env.SnapInstanceName()
	if instanceName == "" { // not a snap
		instanceName = "<snap-instance-name>"
	}
	return fmt.Sprintf("Run \"sudo %s.rag set --package %s=%q\" to configure it.", instanceName, key, value)
}

// SuggestServiceManagement lists the snap commands that manage the snap's services.
func SuggestServiceManagement() string {
	instanceName := env.SnapInstanceName()
	if instanceName == "" { // not a snap
		instanceName = "<snap-instance-name>"
	}

	return fmt.Sprintf("\nUse \"snap logs|start|stop|restart %v\" for service management.\n", instanceName)
}

// SuggestSavePageAsFile suggests ingesting a JavaScript-rendered page from a
// saved copy instead of fetching it.
func SuggestSavePageAsFile() string {
	return "The page likely requires JavaScript to render its content.\n" +
		"Try rendering it in a browser, saving the page as HTML, then using --file instead."
}

// SuggestListQuestionFormat suggests the list-based question format when Tika
// found no HTML tables in a document.
func SuggestListQuestionFormat() string {
	return "Tip: Tika may not recognize PDF tables as HTML <table> elements.\n" +
		"Re-run and choose 'Numbered / bulleted list or question marks' instead."
}

// SuggestEnableUI returns the commands that enable the daemon's loopback
// listener, which serves the local UI, for the snap named snapName.
func SuggestEnableUI(snapName string) string {
	return fmt.Sprintf("Enable it with:\n  sudo %[1]s set api.loopback.enabled=true\n  sudo snap restart %[1]s.ragd\n\nThen run `%[1]s ui` again.", snapName)
}
