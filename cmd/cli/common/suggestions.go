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

func SuggestServiceManagement() string {

	instanceName := env.SnapInstanceName()
	if instanceName == "" { // not a snap
		instanceName = "<snap-instance-name>"
	}

	return fmt.Sprintf("\nUse \"snap logs|start|stop|restart %v\" for service management.\n", instanceName)
}
