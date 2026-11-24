package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/sreenivas-sadhu-prabhakara/batch-near-expiry-guardian/internal/stock"
)

// Store keeps the book in memory and on disk in one plain JSON file.
//
// There is no database and no server. The file sits wherever the owner points
// -data at, so it can live in a folder that is already being backed up or on a
// pen drive that goes home at night. Writes go to a temporary file in the same
// directory and are then renamed over the old one, so a power cut or a full
// disk leaves the previous good file intact rather than half a file.
type Store struct {
	mu   sync.RWMutex
	path string
	book *stock.Book
}

// OpenStore loads the data file, or starts an empty book if there is not one
// yet. The file is not created until something is saved.
func OpenStore(path string) (*Store, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("data file %s: %w", path, err)
	}
	s := &Store{path: absolute, book: stock.NewBook()}

	raw, err := os.ReadFile(absolute)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read the data file %s: %w", absolute, err)
	}
	if len(raw) == 0 {
		return s, nil
	}
	loaded := stock.NewBook()
	if err := json.Unmarshal(raw, loaded); err != nil {
		return nil, fmt.Errorf("the data file %s is not valid JSON: %w", absolute, err)
	}
	loaded.Normalise()
	s.book = loaded
	return s, nil
}

// Path is where the data file lives. The interface shows it, so the owner
// always knows which file they are looking at.
func (s *Store) Path() string { return s.path }

// Read runs fn against the book under a read lock. fn must not keep the
// pointer or change anything behind it.
func (s *Store) Read(fn func(bk *stock.Book)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.book)
}

// Write runs fn against the book under a write lock and saves the file if fn
// succeeds. Nothing is saved when fn fails, and the domain validates an edit
// before it touches a record, so a rejected edit changes nothing in memory
// either.
func (s *Store) Write(fn func(bk *stock.Book) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(s.book); err != nil {
		return err
	}
	return s.save()
}

// ReplaceBatches swaps every batch for the ones given, which is what a CSV
// import does: it replaces the stock list rather than merging into it, because
// merging two counts of the same shelf is how a count goes wrong.
//
// The shop's own policy is kept. A stock sheet says nothing about return terms.
func (s *Store) ReplaceBatches(batches []stock.Batch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	replacement := &stock.Book{Policy: s.book.Policy, Batches: batches}
	replacement.Normalise()
	s.book = replacement
	return s.save()
}

// save writes the data file atomically: a temporary file in the same directory,
// flushed to disk, then renamed over the target.
func (s *Store) save() error {
	raw, err := json.MarshalIndent(s.book, "", "  ")
	if err != nil {
		return fmt.Errorf("could not encode the book: %w", err)
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("could not write beside %s: %w", s.path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // harmless once the rename has happened

	// The file holds a shop's purchase prices and supplier terms, so it is
	// readable by its owner and nobody else.
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("could not set permissions on %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("could not write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("could not flush %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("could not replace %s: %w", s.path, err)
	}
	return nil
}
