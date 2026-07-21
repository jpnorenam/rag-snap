package common

import (
	"fmt"

	"github.com/canonical/go-snapctl/env"
)

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

func SuggestServerLogs() string {
	return fmt.Sprintf("Run \"snap logs %s\" to see the server logs.", daemonServiceName())
}

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

func SuggestServiceManagement() string {

	instanceName := env.SnapInstanceName()
	if instanceName == "" { // not a snap
		instanceName = "<snap-instance-name>"
	}

	return fmt.Sprintf("\nUse \"snap logs|start|stop|restart %v\" for service management.\n", instanceName)
}
