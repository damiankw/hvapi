// Package logfile is an io.Writer for a log file that rotates by size, so
// the log can never grow without limit on a host that runs for years.
package logfile

import (
	"fmt"
	"os"
	"sync"
)

// Writer appends to a file and rotates it when it exceeds MaxBytes:
// name -> name.1, name.1 -> name.2 ... up to Keep files; older are removed.
type Writer struct {
	Name     string
	MaxBytes int64
	Keep     int

	mu   sync.Mutex
	f    *os.File
	size int64
}

// Open creates or appends to name. keep of 0 means rotate by truncation.
func Open(name string, maxBytes int64, keep int) (*Writer, error) {
	w := &Writer{Name: name, MaxBytes: maxBytes, Keep: keep}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) open() error {
	f, err := os.OpenFile(w.Name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.MaxBytes > 0 && w.size+int64(len(p)) > w.MaxBytes && w.size > 0 {
		if err := w.rotate(); err != nil {
			// Keep logging to the current file rather than lose the line.
			fmt.Fprintf(os.Stderr, "hvapi: log rotation failed: %v\n", err)
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate closes the current file, shifts the numbered backups and opens a
// fresh file. Rename is atomic enough for a single writer.
func (w *Writer) rotate() error {
	_ = w.f.Close()
	if w.Keep <= 0 {
		if err := os.Remove(w.Name); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else {
		_ = os.Remove(backup(w.Name, w.Keep))
		for i := w.Keep - 1; i >= 1; i-- {
			if _, err := os.Stat(backup(w.Name, i)); err == nil {
				if err := os.Rename(backup(w.Name, i), backup(w.Name, i+1)); err != nil {
					return err
				}
			}
		}
		if err := os.Rename(w.Name, backup(w.Name, 1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return w.open()
}

func backup(name string, i int) string { return fmt.Sprintf("%s.%d", name, i) }

// Close closes the file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
