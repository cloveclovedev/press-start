package settings

import (
	"log/slog"
	"sync"
)

// Store reads the settings file again on each call to Current, so a changed
// file is used without a restart (PS-ALARM-010.1). When the file cannot be
// read, Store keeps the last good settings (PS-ALARM-010.2).
type Store struct {
	path   string
	logger *slog.Logger

	mu         sync.Mutex
	last       Settings
	unreadable bool
}

// NewStore loads the settings file at path once. It returns an error when
// that first load fails, because no good settings exist then.
func NewStore(path string, logger *slog.Logger) (*Store, error) {
	s, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, logger: logger, last: s}, nil
}

// Current reads the settings file again and returns its settings. When the
// read or the check fails, Current returns the last good settings. It logs
// the failure only when the file changes from readable to unreadable.
func (st *Store) Current() Settings {
	s, err := Load(st.path)

	st.mu.Lock()
	defer st.mu.Unlock()
	if err != nil {
		if !st.unreadable {
			st.logger.Error("settings unreadable", "path", st.path, "error", err)
			st.unreadable = true
		}
		return st.last
	}
	st.last = s
	st.unreadable = false
	return s
}
