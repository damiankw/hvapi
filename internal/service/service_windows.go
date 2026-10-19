//go:build windows

// Package service installs, removes and runs hvapi as a Windows service.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	Name        = "hvapi"
	DisplayName = "Hyper-V Diagnostics API"
	Description = "Read-only HTTP API exposing Hyper-V diagnostic data."
)

// IsService reports whether the process was started by the Service Control Manager.
func IsService() (bool, error) {
	return svc.IsWindowsService()
}

// Install registers the service to start automatically and restart on failure.
// It runs as LocalSystem; change the account afterwards with
// sc.exe config hvapi obj= "DOMAIN\account" password= "..."
func Install(exePath, configPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(Name); err == nil {
		s.Close()
		return fmt.Errorf("service %q already exists", Name)
	}

	s, err := m.CreateService(Name, exePath, mgr.Config{
		DisplayName:  DisplayName,
		Description:  Description,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}, "run", "-config", configPath)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	// Restart after 5 s, then 30 s, then 60 s; reset the failure count after a day.
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 86400)
	return nil
}

// Uninstall stops the service if it is running, then removes it.
func Uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(Name)
	if err != nil {
		return fmt.Errorf("service %q not found", Name)
	}
	defer s.Close()

	if st, err := s.Control(svc.Stop); err == nil {
		for i := 0; i < 20 && st.State != svc.Stopped; i++ {
			time.Sleep(500 * time.Millisecond)
			if st, err = s.Query(); err != nil {
				break
			}
		}
	} else if !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("stop service: %w", err)
	}
	return s.Delete()
}

// Run hands control to the Service Control Manager. work runs until its context
// is cancelled by a Stop or Shutdown request.
func Run(work func(ctx context.Context) error) error {
	return svc.Run(Name, &handler{work: work})
}

type handler struct {
	work func(ctx context.Context) error
}

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.work(ctx) }()

	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case err := <-done:
			// The server stopped on its own: report failure so recovery actions apply.
			if err != nil {
				return true, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(15 * time.Second):
				}
				return false, 0
			}
		}
	}
}
