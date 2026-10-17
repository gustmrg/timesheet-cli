package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeSystem struct {
	calls  []string
	linger string
	active string
	fail   string
}

func (f *fakeSystem) run(name string, args ...string) (string, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, call)
	if f.fail != "" && strings.Contains(call, f.fail) {
		return "Failed to connect to bus", errors.New("exit status 1")
	}
	if name == "loginctl" {
		return f.linger + "\n", nil
	}
	if len(args) > 1 && args[1] == "is-active" {
		if f.active != "active" {
			return f.active + "\n", errors.New("exit status 3")
		}
		return "active\n", nil
	}
	return "", nil
}

// newServiceTestApp points the unit directory, home, and executable at
// temporary paths and replaces systemctl and loginctl, so tests never touch
// the real user manager.
func newServiceTestApp(t *testing.T, system *fakeSystem) (*app, string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the MCP service is Linux only")
	}
	directory := t.TempDir()
	t.Setenv("HOME", filepath.Join(directory, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(directory, "config"))
	for _, name := range mcpServiceEnvironment {
		t.Setenv(name, "")
	}
	executable := filepath.Join(directory, "bin dir", "timesheet")
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := currentExecutable
	currentExecutable = func() (string, error) { return executable, nil }
	t.Cleanup(func() { currentExecutable = previous })
	a := &app{errOut: io.Discard, runCommand: system.run}
	return a, filepath.Join(directory, "config", "systemd", "user", mcpServiceName), executable
}

func TestMCPInstallWritesTheUnitAndStartsIt(t *testing.T) {
	system := &fakeSystem{linger: "yes"}
	a, unitPath, executable := newServiceTestApp(t, system)
	t.Setenv("TIMESHEET_BASE_URL", "https://example.test/100%")
	t.Setenv("TIMESHEET_PASS", "must-not-leak")

	result, err := a.installMCPService("127.0.0.1:9000", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Installed || !result.Linger || result.URL != "http://127.0.0.1:9000/mcp" || result.Unit != unitPath {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(result.TokenFile); err != nil {
		t.Fatalf("token file was not created: %v", err)
	}
	body, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	unit := string(body)
	for _, fragment := range []string{
		`ExecStart="` + executable + `" mcp --http 127.0.0.1:9000 --token-file ` + result.TokenFile,
		`Environment=TIMESHEET_BASE_URL=https://example.test/100%%`,
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(unit, fragment) {
			t.Errorf("unit missing %q:\n%s", fragment, unit)
		}
	}
	if strings.Contains(unit, "must-not-leak") {
		t.Error("the unit must not contain TIMESHEET_PASS")
	}
	want := []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable " + mcpServiceName,
		"systemctl --user restart " + mcpServiceName,
	}
	if strings.Join(system.calls[:3], "\n") != strings.Join(want, "\n") {
		t.Errorf("calls = %q", system.calls)
	}
	if len(a.collectWarnings()) != 0 {
		t.Errorf("unexpected warnings %+v", a.collectWarnings())
	}
}

func TestMCPInstallWarnsWithoutLingerAndReportsSystemctlFailures(t *testing.T) {
	system := &fakeSystem{linger: "no"}
	a, _, _ := newServiceTestApp(t, system)
	result, err := a.installMCPService(mcpDefaultAddress, "", false)
	if err != nil || result.Linger {
		t.Fatalf("install = %+v, %v", result, err)
	}
	if warnings := a.collectWarnings(); len(warnings) != 1 || warnings[0].Code != "linger_disabled" {
		t.Errorf("warnings = %+v", warnings)
	}

	a, _, _ = newServiceTestApp(t, &fakeSystem{fail: "daemon-reload"})
	_, err = a.installMCPService(mcpDefaultAddress, "", false)
	if commandErr := classify(err); commandErr.code != "operation_failed" || !strings.Contains(commandErr.message, "Failed to connect to bus") {
		t.Errorf("systemctl failure = %+v", commandErr)
	}
}

func TestMCPInstallRejectsInvalidInputBeforeTouchingSystemd(t *testing.T) {
	for _, address := range []string{"8765", ":8765", "localhost:http", "127.0.0.1:70000"} {
		system := &fakeSystem{}
		a, unitPath, _ := newServiceTestApp(t, system)
		_, err := a.installMCPService(address, "", false)
		if classify(err).code != "invalid_input" || len(system.calls) != 0 {
			t.Errorf("address %q = %v, calls %q", address, err, system.calls)
		}
		if _, statErr := os.Stat(unitPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Errorf("address %q wrote a unit", address)
		}
	}

	system := &fakeSystem{}
	a, _, _ := newServiceTestApp(t, system)
	if _, err := a.installMCPService(mcpDefaultAddress, filepath.Join(t.TempDir(), "missing"), true); err == nil || len(system.calls) != 0 {
		t.Errorf("missing explicit token file = %v, calls %q", err, system.calls)
	}

	currentExecutable = func() (string, error) { return filepath.Join(os.TempDir(), "go-build123", "exe", "timesheet"), nil }
	if _, err := a.installMCPService(mcpDefaultAddress, "", false); err == nil {
		t.Error("an executable built by go run must be refused")
	}
}

func TestMCPUninstallRemovesTheUnit(t *testing.T) {
	system := &fakeSystem{linger: "yes"}
	a, unitPath, _ := newServiceTestApp(t, system)
	result, err := a.uninstallMCPService()
	if err != nil || result.Removed || len(system.calls) != 0 {
		t.Fatalf("uninstall without a unit = %+v, %v, calls %q", result, err, system.calls)
	}

	install, err := a.installMCPService(mcpDefaultAddress, "", false)
	if err != nil {
		t.Fatal(err)
	}
	system.calls = nil
	result, err = a.uninstallMCPService()
	if err != nil || !result.Removed {
		t.Fatalf("uninstall = %+v, %v", result, err)
	}
	if _, err := os.Stat(unitPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("the unit file is still present")
	}
	if _, err := os.Stat(install.TokenFile); err != nil {
		t.Error("uninstall must keep the token file")
	}
	want := "systemctl --user disable --now " + mcpServiceName + "\nsystemctl --user daemon-reload"
	if strings.Join(system.calls, "\n") != want {
		t.Errorf("calls = %q", system.calls)
	}
}

func TestSystemdQuoteEscapesSpecialCharacters(t *testing.T) {
	for input, want := range map[string]string{
		"/usr/local/bin/timesheet": "/usr/local/bin/timesheet",
		"/opt/my tools/timesheet":  `"/opt/my tools/timesheet"`,
		`/a"b`:                     `"/a\"b"`,
		"/cost/100%":               "/cost/100%%",
		"$HOME/x":                  "$$HOME/x",
	} {
		if got := systemdQuote(input); got != want {
			t.Errorf("systemdQuote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUpgradeRestartsOnlyARunningMCPService(t *testing.T) {
	t.Setenv("SUDO_USER", "")
	system := &fakeSystem{linger: "yes", active: "active"}
	a, _, _ := newServiceTestApp(t, system)
	if restarted := a.restartMCPServiceAfterUpgrade(); restarted != nil || len(system.calls) != 0 {
		t.Fatalf("without a unit = %v, calls %q", restarted, system.calls)
	}

	if _, err := a.installMCPService(mcpDefaultAddress, "", false); err != nil {
		t.Fatal(err)
	}
	system.calls = nil
	if restarted := a.restartMCPServiceAfterUpgrade(); restarted == nil || !*restarted {
		t.Fatalf("running service restarted = %v", restarted)
	}
	want := "systemctl --user is-active " + mcpServiceName + "\nsystemctl --user restart " + mcpServiceName
	if strings.Join(system.calls, "\n") != want {
		t.Errorf("calls = %q", system.calls)
	}

	system.calls, system.active = nil, "inactive"
	if restarted := a.restartMCPServiceAfterUpgrade(); restarted == nil || *restarted || len(system.calls) != 1 {
		t.Errorf("stopped service = %v, calls %q", restarted, system.calls)
	}

	system.calls, system.active, system.fail = nil, "active", "restart"
	a.warnings = nil
	if restarted := a.restartMCPServiceAfterUpgrade(); restarted == nil || *restarted {
		t.Errorf("failed restart = %v", restarted)
	}
	if warnings := a.collectWarnings(); len(warnings) != 1 || warnings[0].Code != "service_not_restarted" {
		t.Errorf("warnings = %+v", warnings)
	}
}
