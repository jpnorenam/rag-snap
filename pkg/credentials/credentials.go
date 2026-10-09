// Package credentials resolves the CLI's backend secrets. Environment variables
// always win; when a variable is unset and the fallback has been enabled (only
// the rag-cli CLI enables it), the value is read from a per-user JSON file.
// Processes that never call Enable, such as ragd, get plain environment lookups.
package credentials

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"syscall"
)

// FileName is the credentials file name inside $SNAP_USER_COMMON.
const FileName = "credentials.json"

var supported = map[string]bool{
	"OPENSEARCH_USERNAME": true,
	"OPENSEARCH_PASSWORD": true,
	"CHAT_API_KEY":        true,
}

var (
	mu      sync.Mutex
	path    string
	loaded  bool
	values  map[string]string
	loadErr error
)

// Enable turns on the file fallback for this process, reading from filePath
// when a requested variable is not set in the environment.
func Enable(filePath string) {
	mu.Lock()
	defer mu.Unlock()
	path, loaded, values, loadErr = filePath, false, nil, nil
}

// Path returns the fallback file path, or "" when the fallback is disabled.
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return path
}

// Lookup returns the value of name. An environment variable that is set, even
// to an empty string, wins without touching the file. The file is read and
// validated only when the fallback is enabled and the variable is unset; a
// missing file is not an error.
func Lookup(name string) (string, bool, error) {
	if v, ok := os.LookupEnv(name); ok {
		return v, true, nil
	}
	mu.Lock()
	defer mu.Unlock()
	if path == "" {
		return "", false, nil
	}
	if !loaded {
		values, loadErr = readFile(path)
		loaded = true
	}
	if loadErr != nil {
		return "", false, loadErr
	}
	v, ok := values[name]
	return v, ok, nil
}

func readFile(p string) (map[string]string, error) {
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("credentials file %s: %w", p, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("credentials file %s must be a regular file, not a symlink or special file", p)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return nil, fmt.Errorf("credentials file %s must be owned by the current user (uid %d)", p, os.Geteuid())
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("credentials file %s must not be accessible by group or others (run: chmod 600 %s)", p, p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("credentials file %s: %w", p, err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		// Report only the position: json errors can quote file content.
		var se *json.SyntaxError
		if errors.As(err, &se) {
			return nil, fmt.Errorf("credentials file %s is not valid JSON (error at byte %d)", p, se.Offset)
		}
		return nil, fmt.Errorf("credentials file %s must contain a single JSON object", p)
	}
	if raw == nil { // the file is the JSON literal null
		return nil, fmt.Errorf("credentials file %s must contain a single JSON object", p)
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if !supported[k] {
			return nil, fmt.Errorf("credentials file %s contains unsupported key %q (allowed: OPENSEARCH_USERNAME, OPENSEARCH_PASSWORD, CHAT_API_KEY)", p, k)
		}
		// json.Unmarshal accepts null for a string, so require a JSON string literal.
		var s string
		if v = bytes.TrimSpace(v); len(v) == 0 || v[0] != '"' || json.Unmarshal(v, &s) != nil {
			return nil, fmt.Errorf("credentials file %s: value of %q must be a string", p, k)
		}
		out[k] = s
	}
	return out, nil
}
