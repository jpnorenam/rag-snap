package common

import (
	"context"
	"os/exec"
	"runtime"
)

// OpenBrowser makes a best-effort attempt to open rawURL in the user's default
// browser (xdg-open on Linux, which snapd routes to the host session under strict
// confinement; open on macOS). It does nothing on other platforms and ignores
// failures: callers always print the URL so the user can open it by hand.
func OpenBrowser(rawURL string) {
	var cmd string
	switch runtime.GOOS {
	case "linux":
		cmd = "xdg-open"
	case "darwin":
		cmd = "open"
	default:
		return
	}
	// The browser outlives this call, so it is started without a cancelable context.
	_ = exec.CommandContext(context.Background(), cmd, rawURL).Start()
}
