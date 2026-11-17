package todo

import (
	"os"
	"path/filepath"
	"time"
)

// Save atomically replaces path in its own directory so external editors see a
// complete Markdown document rather than a partially written one.
func Save(data Data, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.WriteString(Serialize(data)); err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// LoadWithRollover creates a missing document, applies date rollover, and
// persists the normalized/rolled document when needed.
func LoadWithRollover(path string, now time.Time) (Data, error) {
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fresh := Empty(TodayDate(now))
		return fresh, Save(fresh, path)
	}
	if err != nil {
		return Empty(TodayDate(now)), err
	}
	loaded := Parse(string(contents))
	rolled, changed := Rollover(loaded, now)
	if changed {
		if err := Save(rolled, path); err != nil {
			return rolled, err
		}
	}
	return rolled, nil
}
