// Package awssetup exports the bundled AWS setup assets (host script, guest
// bootstrap script, Terraform templates) into a user-owned deployment directory.
// The assets run outside snap confinement; this package only writes files.
package awssetup

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

//go:embed assets
var assets embed.FS

// ContextFile records the snap context the unconfined host script needs.
const ContextFile = "snap-context.env"

// Write exports the assets into dir, creating it (mode 0700) when absent. It
// replaces only the shipped asset files and the context file; saved answers,
// secrets, keys, Terraform state and logs are never touched.
func Write(dir, instance, userCommon string) error {
	if instance == "" || userCommon == "" {
		return errors.New("SNAP_INSTANCE_NAME and SNAP_USER_COMMON are not set; run this command from the installed rag-cli snap")
	}
	if err := checkDir(dir); err != nil {
		return err
	}

	err := fs.WalkDir(assets, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, "assets"), "/")
		if rel == "" {
			return nil
		}
		dest := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o700)
		}
		data, err := assets.ReadFile(p)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o600)
		if rel == "opensearch-on-aws.sh" {
			mode = 0o700
		}
		return writeFile(dest, data, mode)
	})
	if err != nil {
		return fmt.Errorf("exporting setup assets: %w", err)
	}

	ctx := fmt.Sprintf("RAG_SNAP_INSTANCE=%s\nRAG_SNAP_USER_COMMON=%s\n", instance, userCommon)
	return writeFile(filepath.Join(dir, ContextFile), []byte(ctx), 0o600)
}

// checkDir rejects existing directories that are not owned by the caller or
// are writable by group or others.
func checkDir(dir string) error {
	fi, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return os.MkdirAll(dir, 0o700)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("%s is not owned by the current user", dir)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is writable by group or others; run chmod 700 %s", dir, dir)
	}
	return nil
}

// writeFile replaces dest atomically with data and the given mode.
func writeFile(dest string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // best-effort cleanup of the temp file
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}
