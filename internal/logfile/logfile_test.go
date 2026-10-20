package logfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotate(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "x.log")
	w, err := Open(name, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("a", 39) + "\n" // 40 bytes
	for i := 0; i < 10; i++ {              // 400 bytes -> several rotations
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 3 { // x.log, x.log.1, x.log.2 and nothing older
		t.Fatalf("files = %v", names)
	}
	for _, n := range []string{"x.log", "x.log.1", "x.log.2"} {
		st, err := os.Stat(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		if st.Size() > 100 || st.Size() == 0 {
			t.Errorf("%s is %d bytes", n, st.Size())
		}
	}

	// Appending to an existing file counts its current size.
	w, err = Open(name, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		_, _ = w.Write([]byte(line))
	}
	w.Close()
	if st, _ := os.Stat(name); st.Size() > 100 {
		t.Errorf("keep=0 should truncate, size %d", st.Size())
	}
}
