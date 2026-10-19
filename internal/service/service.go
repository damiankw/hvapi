// Package service installs, removes and runs hvapi as a Windows service.
// On other operating systems every call reports that it is unsupported.
package service

const (
	Name        = "hvapi"
	DisplayName = "Hyper-V Diagnostics API"
	Description = "Read-only HTTP API exposing Hyper-V diagnostic data."
)
