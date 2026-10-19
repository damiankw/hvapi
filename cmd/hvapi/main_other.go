//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "hvapi only runs on Windows. Build with GOOS=windows.")
	os.Exit(1)
}
