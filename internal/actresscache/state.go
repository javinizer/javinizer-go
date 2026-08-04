package actresscache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	stateReadFile       = os.ReadFile
	stateRepairTail     = repairStateTail
	stateMkdirAll       = os.MkdirAll
	stateOpenFile       = os.OpenFile
	stateOpenRepairFile = os.OpenFile
	stateTruncate       = func(file *os.File, size int64) error { return file.Truncate(size) }
	stateSeekEnd        = func(file *os.File) error { _, err := file.Seek(0, io.SeekEnd); return err }
	stateWriteNewline   = func(file *os.File) error { _, err := file.Write([]byte{10}); return err }
	stateRename         = os.Rename
)

type stateStore struct {
	mu      sync.Mutex
	entries map[string]StateEntry
	file    *os.File
	encoder *json.Encoder
}

func openState(path string) (*stateStore, error) {
	store := &stateStore{entries: make(map[string]StateEntry)}
	if path == "" {
		return store, nil
	}
	data, err := stateReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return openStateWriter(path, store)
	}
	if err != nil {
		return nil, err
	}
	incompleteTail, err := parseState(data, store.entries)
	if err != nil {
		// Mid-file corruption: quarantine the file and rebuild from scratch
		// instead of failing every build until manual deletion.
		corruptPath := path + ".corrupt"
		if renameErr := stateRename(path, corruptPath); renameErr != nil {
			return nil, fmt.Errorf("parse state: %w (quarantine to %s failed: %w)", err, corruptPath, renameErr)
		}
		log.Printf("actresscache: state file %s corrupt (%v); quarantined to %s", path, err, corruptPath)
		return openStateWriter(path, &stateStore{entries: make(map[string]StateEntry)})
	}
	if err := stateRepairTail(path, data, incompleteTail); err != nil {
		return nil, err
	}
	return openStateWriter(path, store)
}

func openStateWriter(path string, store *stateStore) (*stateStore, error) {
	if err := stateMkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	file, err := stateOpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	store.file = file
	store.encoder = json.NewEncoder(file)
	return store, nil
}
func parseState(data []byte, entries map[string]StateEntry) (bool, error) {
	lines := bytes.Split(data, []byte{10})
	for index, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var entry StateEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			if index == len(lines)-1 && !bytes.HasSuffix(data, []byte{10}) {
				return true, nil
			}
			return false, fmt.Errorf("parse state line %d: %w", index+1, err)
		}
		if entry.Key != "" {
			entries[entry.Key] = entry
		}
	}
	return false, nil
}

func repairStateTail(path string, data []byte, incomplete bool) error {
	if len(data) == 0 || bytes.HasSuffix(data, []byte{10}) {
		return nil
	}
	file, err := stateOpenRepairFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if incomplete {
		cutoff := bytes.LastIndexByte(data, 10) + 1
		if err := stateTruncate(file, int64(cutoff)); err != nil {
			return err
		}
	} else {
		if err := stateSeekEnd(file); err != nil {
			return err
		}
		if err := stateWriteNewline(file); err != nil {
			return err
		}
	}
	return file.Sync()
}

func (s *stateStore) get(key string) (StateEntry, bool) {
	if s == nil {
		return StateEntry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	return entry, ok
}

func (s *stateStore) append(entry StateEntry) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[entry.Key] = entry
	if s.encoder == nil {
		return nil
	}
	if err := s.encoder.Encode(entry); err != nil {
		return err
	}
	return s.file.Sync()
}

func (s *stateStore) close() error {
	if s == nil || s.file == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}

//nolint:unused // used by tests
func readState(reader io.Reader, entries map[string]StateEntry) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	content := string(data)
	endsWithNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry StateEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			if i == len(lines)-1 && !endsWithNewline {
				continue
			}
			return err
		}
		if entry.Key != "" {
			entries[entry.Key] = entry
		}
	}
	return nil
}
