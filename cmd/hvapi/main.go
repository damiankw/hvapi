//go:build windows

// Command hvapi is a read-only Hyper-V diagnostics API.
//
//	hvapi run        [-config path]   run in the console (or as the service)
//	hvapi install    [-config path]   register the Windows service
//	hvapi uninstall                   stop and remove the Windows service
//	hvapi hash-key                    generate a new API key and its hash
//	hvapi version                     print build information
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/damiankw/hvapi/internal/auth"
	"github.com/damiankw/hvapi/internal/config"
	"github.com/damiankw/hvapi/internal/server"
	"github.com/damiankw/hvapi/internal/service"
	"github.com/damiankw/hvapi/internal/version"
)

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func dispatch(args []string) error {
	// Started by the Service Control Manager: run as the service.
	if isSvc, err := service.IsService(); err == nil && isSvc {
		cfgPath := defaultConfigPath()
		if len(args) > 0 && args[0] == "run" {
			fs := flag.NewFlagSet("run", flag.ContinueOnError)
			p := fs.String("config", cfgPath, "")
			_ = fs.Parse(args[1:])
			cfgPath = *p
		}
		return service.Run(func(ctx context.Context) error { return serve(ctx, cfgPath, true) })
	}

	if len(args) == 0 {
		usage()
		return nil
	}

	switch args[0] {
	case "run":
		fs := flag.NewFlagSet("run", flag.ExitOnError)
		cfgPath := fs.String("config", defaultConfigPath(), "path to config file")
		_ = fs.Parse(args[1:])
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return serve(ctx, *cfgPath, false)

	case "install":
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		cfgPath := fs.String("config", defaultConfigPath(), "path to config file")
		_ = fs.Parse(args[1:])
		abs, err := filepath.Abs(*cfgPath)
		if err != nil {
			return err
		}
		if _, err := config.Load(abs); err != nil {
			return fmt.Errorf("config check failed, not installing:\n%w", err)
		}
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := service.Install(exe, abs); err != nil {
			return err
		}
		fmt.Printf("Installed service %q using %s\nStart it with: sc.exe start %s\n", service.Name, abs, service.Name)
		return nil

	case "uninstall":
		if err := service.Uninstall(); err != nil {
			return err
		}
		fmt.Printf("Removed service %q\n", service.Name)
		return nil

	case "hash-key":
		key, hash, err := auth.GenerateKey()
		if err != nil {
			return err
		}
		fmt.Printf("API key (give this to the client, it is shown once):\n  %s\n\n", key)
		fmt.Printf("SHA-256 hash (put this in config.json under api_keys):\n  %s\n", hash)
		return nil

	case "version":
		fmt.Printf("hvapi %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
		return nil

	case "help", "-h", "--help", "/?":
		usage()
		return nil
	}

	usage()
	return fmt.Errorf("unknown command %q", args[0])
}

// serve loads the config and runs the HTTP server until ctx is cancelled.
func serve(ctx context.Context, cfgPath string, asService bool) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	log, closeLog, err := newLogger(cfg, cfgPath, asService)
	if err != nil {
		return err
	}
	defer closeLog()

	if host, _, _ := net.SplitHostPort(cfg.Listen); host == "" || host == "0.0.0.0" || host == "::" {
		log.Warn("listening on all interfaces; bind to the management NIC's IP instead", "listen", cfg.Listen)
	}

	srv := server.New(cfg, log)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Info("hvapi started", "version", version.Version, "listen", cfg.Listen, "keys", len(cfg.KeyIDs))

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		log.Error("server stopped", "error", err)
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		log.Info("hvapi stopping")
		return srv.Shutdown(shutdownCtx)
	}
}

// newLogger writes JSON logs to the configured file (relative paths sit next to
// the config file). In the console with no log file set, it logs to stdout.
func newLogger(cfg *config.Config, cfgPath string, asService bool) (*slog.Logger, func(), error) {
	path := cfg.LogFile
	if path == "" && asService {
		path = "hvapi.log"
	}
	var out io.Writer = os.Stdout
	closeFn := func() {}
	if path != "" {
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(cfgPath), path)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file: %w", err)
		}
		out = f
		if !asService {
			out = io.MultiWriter(f, os.Stdout)
		}
		closeFn = func() { _ = f.Close() }
	}
	return slog.New(slog.NewJSONHandler(out, nil)), closeFn, nil
}

func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(filepath.Dir(exe), "config.json")
}

func usage() {
	fmt.Print(`hvapi - read-only Hyper-V diagnostics API

Usage:
  hvapi run [-config path]       Run in this console (Ctrl+C to stop)
  hvapi install [-config path]   Install as a Windows service (run as Administrator)
  hvapi uninstall                Stop and remove the Windows service
  hvapi hash-key                 Generate a new API key and its hash
  hvapi version                  Print build information

The config file defaults to config.json next to hvapi.exe.
`)
}
