package stats

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SidecarPath returns the conventional sidecar location for an EDB at
// edbPath. We append ".stats" rather than swapping the extension so
// `<x>.db` and `<x>.db.stats` sit visibly together in `ls`.
func SidecarPath(edbPath string) string { return edbPath + ".stats" }

// Save atomically writes s to SidecarPath(edbPath). Implementation:
// write to a temp file in the same directory, fsync, rename.
//
// Concurrency: undefined for concurrent Save calls against the same
// edbPath. POSIX rename(2) is atomic at the destination, so the
// final sidecar will be one of the writers' outputs in its entirety
// (no torn writes), but which writer wins is unspecified. The
// previous version of this code created a `.stats.lock` marker file
// before writing; that was security-blanket theatre — `os.Create` is
// not atomic, the lock was never read by anyone, and the only real
// invariant (single-writer-wins, no partial files) is already
// provided by the temp-file + rename pattern. The lock has been
// removed.
func Save(edbPath string, s *Schema) error {
	if s == nil {
		return fmt.Errorf("stats.Save: nil schema")
	}
	out := SidecarPath(edbPath)
	dir := filepath.Dir(out)

	tmp, err := os.CreateTemp(dir, ".tsq-stats-*.tmp")
	if err != nil {
		return fmt.Errorf("stats.Save: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName)
		}
	}()

	if err := Encode(tmp, s); err != nil {
		tmp.Close()
		return fmt.Errorf("stats.Save: encode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("stats.Save: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("stats.Save: close: %w", err)
	}
	if err := os.Rename(tmpName, out); err != nil {
		return fmt.Errorf("stats.Save: rename: %w", err)
	}
	cleanup = false
	return nil
}

// Load reads SidecarPath(edbPath), validates magic+CRC+format-version,
// then validates that the sidecar's EDBHash matches a freshly computed
// hash of edbPath. On any validation failure, the function emits a
// warning to warnW (typically os.Stderr) and returns (nil, err) — the
// caller is expected to treat nil as "default-stats mode" per plan §3.4.
//
// Returning a non-nil schema and a nil error is the only "use this"
// signal. The planner consumer (PR2) layers a default-stats fallback
// on top of this contract.
func Load(edbPath string, warnW io.Writer) (*Schema, error) {
	out := SidecarPath(edbPath)
	buf, err := os.ReadFile(out)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			warnf(warnW, "stats: no sidecar at %s — running in default-stats mode", out)
			return nil, err
		}
		warnf(warnW, "stats: read %s: %v — running in default-stats mode", out, err)
		return nil, err
	}
	s, err := Decode(buf)
	if err != nil {
		warnf(warnW, "stats: decode %s: %v — running in default-stats mode", out, err)
		return nil, err
	}
	// Hash-validate against the live EDB.
	live, err := HashFile(edbPath)
	if err != nil {
		warnf(warnW, "stats: hash %s: %v — running in default-stats mode", edbPath, err)
		return nil, err
	}
	if live != s.EDBHash {
		warnf(warnW, "stats: EDB hash mismatch for %s — sidecar is stale; running in default-stats mode", edbPath)
		return nil, ErrHashMismatch
	}
	return s, nil
}

func warnf(w io.Writer, format string, args ...interface{}) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, "warning: "+format+"\n", args...)
}
