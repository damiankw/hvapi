// Command hvapi is a read-only Hyper-V diagnostics API.
//
// Run 'hvapi help' for the commands.
package main

import (
	"context"
	"encoding/json"
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
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/damiankw/hvapi/internal/auth"
	"github.com/damiankw/hvapi/internal/config"
	"github.com/damiankw/hvapi/internal/hyperv"
	"github.com/damiankw/hvapi/internal/server"
	"github.com/damiankw/hvapi/internal/service"
	"github.com/damiankw/hvapi/internal/setup"
	"github.com/damiankw/hvapi/internal/version"
	"github.com/damiankw/hvapi/internal/wmi"
)

func main() {
	if err := dispatch(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// command is one CLI verb. Order in commands() is the order in 'hvapi help'.
type command struct {
	name  string
	args  string // shown after the name in usage
	short string // one line, for the command list
	long  string // full help, for 'hvapi help <command>'
	run   func(args []string, in io.Reader, out io.Writer) error
}

func commands() []command {
	return []command{
		{
			name: "run", args: "[-config path]",
			short: "Run the API in this console (Ctrl+C to stop)",
			long: `Starts the HTTP server in the foreground using the config file and logs
to the console as well as the log file. Use it to test a configuration
before installing the service. The Service Control Manager also starts
the service through this command.`,
			run: cmdRun,
		},
		{
			name: "configure", args: "[-config path]",
			short: "Build or edit config.json with a guided walk-through",
			long: `Asks for every setting in turn, explains what it means, suggests a
sensible value, and writes the config file. If the file already exists its
values are offered as the defaults, so it can also edit an existing setup.
API keys can be generated on the spot; the key is shown once and only its
hash is stored. Nothing is written until the end, and the result is
validated first.`,
			run: cmdConfigure,
		},
		{
			name: "show-config", args: "[-config path]",
			short: "Print the config with every setting explained",
			long: `Reads the config file and prints each setting with its effective value,
whether a default applies, and what the setting does. Key hashes are
shortened. Problems are listed if the file would not pass validation, and
the exit code is 1 in that case, so it doubles as a config check.`,
			run: cmdShowConfig,
		},
		{
			name:  "hash-key",
			short: "Generate a new API key and its hash",
			long: `Prints a new random API key and its SHA-256 hash. Give the key to the
client and put the hash in config.json under api_keys. The key is not
stored anywhere and cannot be recovered from the hash.`,
			run: cmdHashKey,
		},
		{
			name: "install", args: "[-config path]",
			short: "Install as a Windows service (run as Administrator)",
			long: `Registers hvapi as a Windows service named "` + service.Name + `" that starts
automatically and restarts on failure. The config file is checked first and
its absolute path is baked into the service command line. The service runs
as LocalSystem; change the account afterwards with
  sc.exe config ` + service.Name + ` obj= "DOMAIN\account" password= "..."
Start it with: sc.exe start ` + service.Name,
			run: cmdInstall,
		},
		{
			name:  "uninstall",
			short: "Stop and remove the Windows service",
			long:  `Stops the "` + service.Name + `" service if it is running and removes it. The exe and config stay.`,
			run:   cmdUninstall,
		},
		{
			name: "wmi", args: "[-ns namespace] <WQL query>",
			short: "Run a read-only WMI query and print the rows as JSON",
			long: `Runs one WQL query through the same read-only WMI client the service
uses and prints the instances as JSON. Useful for checking what a host
exposes, or why an endpoint reports a note. Only SELECT, ASSOCIATORS OF and
REFERENCES OF are accepted. The namespace defaults to root\virtualization\v2.
Windows only.

  hvapi wmi "SELECT ElementName, EnabledState FROM Msvm_ComputerSystem"
  hvapi wmi -ns root\cimv2 "SELECT * FROM Win32_OperatingSystem"`,
			run: cmdWMI,
		},
		{
			name: "openapi", args: "[-o file]",
			short: "Print the OpenAPI document for this build",
			long: `Writes the OpenAPI 3.0 description of the API as JSON, the same document
the running service serves at /openapi.json. Use -o to write it to a file.`,
			run: cmdOpenAPI,
		},
		{
			name:  "version",
			short: "Print build information",
			long:  `Prints the version, commit and build date of this binary.`,
			run: func([]string, io.Reader, io.Writer) error {
				fmt.Printf("hvapi %s (commit %s, built %s, %s)\n", version.Version, version.Commit, version.Date, runtime.Version())
				return nil
			},
		},
		{
			name: "help", args: "[command]",
			short: "Show this help, or details of one command",
			long:  `Without an argument lists every command. With one, explains that command.`,
			run:   cmdHelp,
		},
	}
}

func dispatch(args []string, in io.Reader, out io.Writer) error {
	// Started by the Service Control Manager: run as the service.
	if isSvc, err := service.IsService(); err == nil && isSvc {
		cfgPath := defaultConfigPath()
		if len(args) > 0 && args[0] == "run" {
			cfgPath = parseConfigFlag("run", args[1:])
		}
		return service.Run(func(ctx context.Context) error { return serve(ctx, cfgPath, true) })
	}

	if len(args) == 0 {
		usage(out)
		return nil
	}
	name := strings.ToLower(args[0])
	switch name {
	case "-h", "--help", "/?", "-help":
		name = "help"
	case "-v", "--version":
		name = "version"
	}
	for _, c := range commands() {
		if c.name == name {
			return c.run(args[1:], in, out)
		}
	}
	usage(out)
	return fmt.Errorf("unknown command %q", args[0])
}

func cmdRun(args []string, _ io.Reader, _ io.Writer) error {
	cfgPath := parseConfigFlag("run", args)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return serve(ctx, cfgPath, false)
}

func cmdConfigure(args []string, in io.Reader, out io.Writer) error {
	return setup.Configure(in, out, parseConfigFlag("configure", args))
}

func cmdShowConfig(args []string, _ io.Reader, out io.Writer) error {
	return setup.Show(out, parseConfigFlag("show-config", args))
}

func cmdHashKey(_ []string, _ io.Reader, out io.Writer) error {
	key, hash, err := auth.GenerateKey()
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "API key (give this to the client, it is shown once):\n  %s\n\n", key)
	fmt.Fprintf(out, "SHA-256 hash (put this in config.json under api_keys):\n  %s\n", hash)
	return nil
}

func cmdInstall(args []string, _ io.Reader, out io.Writer) error {
	abs, err := filepath.Abs(parseConfigFlag("install", args))
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
	fmt.Fprintf(out, "Installed service %q using %s\nStart it with: sc.exe start %s\n", service.Name, abs, service.Name)
	return nil
}

func cmdUninstall(_ []string, _ io.Reader, out io.Writer) error {
	if err := service.Uninstall(); err != nil {
		return err
	}
	fmt.Fprintf(out, "Removed service %q\n", service.Name)
	return nil
}

func cmdWMI(args []string, _ io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("wmi", flag.ExitOnError)
	ns := fs.String("ns", wmi.NSVirtualization, "WMI namespace")
	_ = fs.Parse(args)
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return errors.New("a WQL query is required, for example: hvapi wmi \"SELECT * FROM Msvm_ComputerSystem\"")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rows, err := wmi.New().Query(ctx, *ns, query)
	if err != nil {
		return err
	}
	if rows == nil {
		rows = []wmi.Row{}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func cmdOpenAPI(args []string, _ io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("openapi", flag.ExitOnError)
	file := fs.String("o", "", "write to this file instead of standard output")
	_ = fs.Parse(args)
	if *file == "" {
		_, err := out.Write(server.Spec())
		return err
	}
	if err := os.WriteFile(*file, server.Spec(), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "Wrote %s\n", *file)
	return nil
}

func cmdHelp(args []string, _ io.Reader, out io.Writer) error {
	if len(args) == 0 {
		usage(out)
		return nil
	}
	name := strings.ToLower(args[0])
	for _, c := range commands() {
		if c.name == name {
			fmt.Fprintf(out, "hvapi %s %s\n\n%s\n\n%s\n", c.name, c.args, c.short, c.long)
			if strings.Contains(c.args, "-config") {
				fmt.Fprintf(out, "\n-config defaults to config.json next to hvapi.exe (%s).\n", defaultConfigPath())
			}
			return nil
		}
	}
	names := make([]string, 0, len(commands()))
	for _, c := range commands() {
		names = append(names, c.name)
	}
	sort.Strings(names)
	return fmt.Errorf("unknown command %q; commands are: %s", args[0], strings.Join(names, ", "))
}

func usage(out io.Writer) {
	fmt.Fprintf(out, "hvapi %s - read-only Hyper-V diagnostics API\n\n", version.Version)
	fmt.Fprintln(out, "Usage: hvapi <command> [options]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Commands:")
	for _, c := range commands() {
		fmt.Fprintf(out, "  %-28s %s\n", strings.TrimSpace(c.name+" "+c.args), c.short)
	}
	fmt.Fprintf(out, `
Getting started:
  hvapi configure           create config.json
  hvapi run                 try it in this console
  hvapi install             install the service (Administrator)

'hvapi help <command>' explains one command. The config file defaults to
config.json next to hvapi.exe. Once running, the API documentation is at
http://<listen address>/docs.
`)
}

func parseConfigFlag(cmd string, args []string) string {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	cfgPath := fs.String("config", defaultConfigPath(), "path to config file")
	_ = fs.Parse(args)
	return *cfgPath
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

	// The poller fills the cache in the background; handlers only read it.
	pollCtx, stopPoll := context.WithCancel(ctx)
	defer stopPoll()
	poller := hyperv.NewPoller(wmi.New(), log,
		time.Duration(cfg.Poll.StateSeconds)*time.Second, time.Duration(cfg.Poll.HeavySeconds)*time.Second)
	go poller.Run(pollCtx)

	srv := server.New(cfg, log, server.Deps{Data: poller.Data, Status: poller.Status})
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Info("hvapi started", "version", version.Version, "listen", cfg.Listen, "keys", len(cfg.KeyIDs), "docs", "http://"+cfg.Listen+"/docs")

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
		path = config.DefaultLogFile
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
