//go:build !windows

package service

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("Windows services are only available on Windows")

// IsService always reports false outside Windows.
func IsService() (bool, error) { return false, nil }

// IsElevated is true outside Windows, where there is no UAC to worry about.
func IsElevated() bool { return true }

func Install(exePath, configPath string) error       { return errUnsupported }
func Uninstall() error                               { return errUnsupported }
func Run(work func(ctx context.Context) error) error { return errUnsupported }
