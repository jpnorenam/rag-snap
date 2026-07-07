// ROOT_PATH is the prefix prepended to every API path, mirroring lxd-ui. It
// defaults to empty so requests are made relative to the origin that served the
// page (same-origin: the ragd loopback listener serves both /ui/ and /1.0/...).
// It is a runtime value, never a build-time host/port, so the embedded assets
// carry no installation-specific URL.
export const ROOT_PATH = "";
