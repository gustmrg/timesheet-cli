package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const (
	mcpServiceName    = "timesheet-mcp.service"
	mcpDefaultAddress = "127.0.0.1:8765"
)

// The service inherits only these variables from the install environment.
// TIMESHEET_USER and TIMESHEET_PASS are deliberately left out: they would be
// written in clear text to the unit file.
var mcpServiceEnvironment = []string{"TIMESHEET_BASE_URL", "TIMESHEET_SESSION", "TIMESHEET_CREDENTIALS"}

// currentExecutable is replaced in tests, whose binary lives in a go-build
// directory that serviceExecutable refuses.
var currentExecutable = os.Executable

type mcpServiceInstall struct {
	Installed bool   `json:"installed"`
	Unit      string `json:"unit"`
	URL       string `json:"url"`
	TokenFile string `json:"tokenFile"`
	Linger    bool   `json:"linger"`
}

type mcpServiceRemoval struct {
	Removed bool   `json:"removed"`
	Unit    string `json:"unit"`
}

func (a *app) mcpInstallCommand() *cobra.Command {
	var listen, tokenFile string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Run the HTTP MCP server as a systemd user service (Linux)",
		Long: `Install timesheet-mcp.service as a systemd user service that runs
"timesheet mcp --http ADDRESS" from this executable, start it now, and start it
again on every login and after a crash. Running it again replaces the service
and restarts it.

The bearer token file is created if needed. Log in first with
"timesheet login --save-credentials", adding "--credential-store file" when the
operating system vault is locked or absent while you are logged out, so the
service can renew its session. Without lingering, systemd stops user services
when your last session ends; the command reports whether it is enabled.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := a.installMCPService(listen, tokenFile, cmd.Flags().Changed("token-file"))
			if err != nil {
				return err
			}
			return a.success(result, func() {
				fmt.Fprintf(a.out, "installed and started %s\n", result.Unit)
				fmt.Fprintf(a.out, "serving %s\n", result.URL)
				fmt.Fprintf(a.out, "bearer token in %s\n", result.TokenFile)
			})
		},
	}
	cmd.Flags().StringVar(&listen, "http", mcpDefaultAddress, "address the service listens on")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "bearer token file (default ~/.timesheet-cli/mcp-token)")
	return cmd
}

func (a *app) mcpUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the systemd user service (Linux)",
		Long:  "Stop, disable, and remove timesheet-mcp.service. The bearer token file, session, and saved credentials are kept.",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			result, err := a.uninstallMCPService()
			if err != nil {
				return err
			}
			return a.success(result, func() {
				if result.Removed {
					fmt.Fprintf(a.out, "stopped and removed %s\n", result.Unit)
				} else {
					fmt.Fprintf(a.out, "%s is not installed\n", result.Unit)
				}
			})
		},
	}
}

func (a *app) installMCPService(address, tokenFile string, explicitTokenFile bool) (mcpServiceInstall, error) {
	if err := requireSystemd(); err != nil {
		return mcpServiceInstall{}, err
	}
	if err := validateListenAddress(address); err != nil {
		return mcpServiceInstall{}, err
	}
	executable, err := serviceExecutable()
	if err != nil {
		return mcpServiceInstall{}, err
	}
	if tokenFile == "" {
		if tokenFile, err = defaultMCPTokenPath(); err != nil {
			return mcpServiceInstall{}, err
		}
	}
	if tokenFile, err = filepath.Abs(tokenFile); err != nil {
		return mcpServiceInstall{}, fail("invalid_input", "resolve token file path", 2, err)
	}
	// The service reads the token file, never TIMESHEET_MCP_TOKEN, so the file
	// must exist and hold a valid token before the service starts.
	if _, err := a.mcpTokenFile(tokenFile, explicitTokenFile); err != nil {
		return mcpServiceInstall{}, err
	}
	unitPath, err := mcpUnitPath()
	if err != nil {
		return mcpServiceInstall{}, err
	}
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return mcpServiceInstall{}, fail("operation_failed", "create systemd user unit directory", 1, err)
	}
	if err := writeFileAtomic(unitPath, []byte(mcpUnit(executable, address, tokenFile)), 0o644); err != nil {
		return mcpServiceInstall{}, fail("operation_failed", "write "+unitPath, 1, err)
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", mcpServiceName},
		{"--user", "restart", mcpServiceName},
	} {
		if err := a.runSystemCommand("systemctl", args...); err != nil {
			return mcpServiceInstall{}, err
		}
	}
	linger := a.lingerEnabled()
	return mcpServiceInstall{Installed: true, Unit: unitPath, URL: "http://" + address + mcpEndpointPath, TokenFile: tokenFile, Linger: linger}, nil
}

func (a *app) uninstallMCPService() (mcpServiceRemoval, error) {
	if err := requireSystemd(); err != nil {
		return mcpServiceRemoval{}, err
	}
	unitPath, err := mcpUnitPath()
	if err != nil {
		return mcpServiceRemoval{}, err
	}
	if _, err := os.Stat(unitPath); errors.Is(err, os.ErrNotExist) {
		return mcpServiceRemoval{Removed: false, Unit: unitPath}, nil
	}
	if err := a.runSystemCommand("systemctl", "--user", "disable", "--now", mcpServiceName); err != nil {
		return mcpServiceRemoval{}, err
	}
	if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return mcpServiceRemoval{}, fail("operation_failed", "remove "+unitPath, 1, err)
	}
	if err := a.runSystemCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return mcpServiceRemoval{}, err
	}
	return mcpServiceRemoval{Removed: true, Unit: unitPath}, nil
}

// lingerEnabled reports whether systemd keeps this user's services running
// while they are logged out, adding a warning when it does not or cannot tell.
func (a *app) lingerEnabled() bool {
	current, err := user.Current()
	if err == nil {
		var output string
		output, err = a.systemCommandOutput("loginctl", "show-user", current.Username, "--property=Linger", "--value")
		if err == nil && strings.TrimSpace(output) == "yes" {
			return true
		}
		if err == nil {
			a.addWarning("linger_disabled", "lingering is off, so the service stops when you log out; run `loginctl enable-linger` to keep it running")
			return false
		}
	}
	a.addWarning("linger_unknown", "could not check lingering; run `loginctl enable-linger` if the service must run while you are logged out")
	return false
}

func (a *app) runSystemCommand(name string, args ...string) error {
	_, err := a.systemCommandOutput(name, args...)
	return err
}

func (a *app) systemCommandOutput(name string, args ...string) (string, error) {
	run := a.runCommand
	if run == nil {
		run = func(name string, args ...string) (string, error) {
			output, err := exec.Command(name, args...).CombinedOutput()
			return string(output), err
		}
	}
	output, err := run(name, args...)
	if err != nil {
		message := fmt.Sprintf("`%s %s` failed", name, strings.Join(args, " "))
		if detail := strings.TrimSpace(output); detail != "" {
			message += ": " + detail
		}
		return output, fail("operation_failed", message, 1, err)
	}
	return output, nil
}

func requireSystemd() error {
	if runtime.GOOS != "linux" {
		return fail("usage", "the MCP service is only supported on Linux with systemd; run `timesheet mcp --http ADDRESS` under your own service manager instead", 2, nil)
	}
	return nil
}

func validateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err == nil {
		var number int
		number, err = strconv.Atoi(port)
		if err == nil && (number <= 0 || number > 65535) {
			err = errors.New("port out of range")
		}
	}
	if err != nil || host == "" {
		return fail("invalid_input", fmt.Sprintf("invalid --http address %q — use HOST:PORT, such as %s", address, mcpDefaultAddress), 2, err)
	}
	return nil
}

func serviceExecutable() (string, error) {
	executable, err := currentExecutable()
	if err == nil {
		executable, err = filepath.EvalSymlinks(executable)
	}
	if err != nil {
		return "", fail("internal_error", "determine current executable", 1, err)
	}
	if strings.Contains(executable, string(filepath.Separator)+"go-build") {
		return "", fail("usage", "run `timesheet mcp install` from an installed executable, not `go run`; the service would point at a temporary build", 2, nil)
	}
	return executable, nil
}

func mcpUnitPath() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fail("internal_error", "determine home directory", 1, err)
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "systemd", "user", mcpServiceName), nil
}

func mcpUnit(executable, address, tokenFile string) string {
	var unit strings.Builder
	unit.WriteString("# Created by `timesheet mcp install`; remove with `timesheet mcp uninstall`.\n")
	unit.WriteString("[Unit]\nDescription=Luby Timesheet MCP server\n\n")
	unit.WriteString("[Service]\n")
	for _, name := range mcpServiceEnvironment {
		if value := os.Getenv(name); value != "" {
			unit.WriteString("Environment=" + systemdQuote(name+"="+value) + "\n")
		}
	}
	unit.WriteString("ExecStart=" + strings.Join([]string{
		systemdQuote(executable), "mcp", "--http", systemdQuote(address), "--token-file", systemdQuote(tokenFile),
	}, " ") + "\n")
	unit.WriteString("Restart=on-failure\nRestartSec=5\n\n")
	unit.WriteString("[Install]\nWantedBy=default.target\n")
	return unit.String()
}

// systemdQuote quotes one unit-file word: specifiers (%) and variable
// expansion ($) are escaped, and words with spaces or quotes are double-quoted.
func systemdQuote(value string) string {
	escaped := strings.NewReplacer("%", "%%", "$", "$$").Replace(value)
	if !strings.ContainsAny(escaped, " \t\"'\\;") {
		return escaped
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(escaped) + `"`
}

func writeFileAtomic(path string, body []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	_, writeErr := temporary.Write(body)
	closeErr := temporary.Close()
	err = errors.Join(writeErr, closeErr)
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}
