package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gustmrg/timesheet-cli/internal/api"
	"github.com/spf13/cobra"
)

const defaultBaseURL = "https://luby-timesheet.azurewebsites.net"

type app struct {
	in      io.Reader
	out     io.Writer
	errOut  io.Writer
	json    bool
	version string
}

type commandError struct {
	code     string
	message  string
	exitCode int
	cause    error
}

func (e *commandError) Error() string { return e.message }
func (e *commandError) Unwrap() error { return e.cause }

func Execute(args []string, in io.Reader, out, errOut io.Writer, version string) int {
	a := &app{in: in, out: out, errOut: errOut, version: version}
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errOut)
	if err := root.Execute(); err != nil {
		commandErr := classify(err)
		if a.json || containsJSONFlag(args) {
			_ = json.NewEncoder(errOut).Encode(map[string]any{
				"ok":    false,
				"error": map[string]string{"code": commandErr.code, "message": commandErr.message},
			})
		} else {
			fmt.Fprintf(errOut, "error: %s\n", commandErr.message)
		}
		return commandErr.exitCode
	}
	return 0
}

func (a *app) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "timesheet",
		Short:         "Manage Luby Timesheet entries",
		Version:       a.version,
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().BoolVar(&a.json, "json", false, "emit machine-readable JSON")
	root.AddCommand(
		a.loginCommand(), a.listCommand(), a.metaCommand(), a.statusCommand(),
		a.addCommand(), a.updateCommand(), a.deleteCommand(), a.versionCommand(),
	)
	return root
}

func (a *app) client() (*api.Client, error) {
	baseURL := os.Getenv("TIMESHEET_BASE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	sessionFile := os.Getenv("TIMESHEET_SESSION")
	if sessionFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fail("internal_error", "determine home directory", 1, err)
		}
		sessionFile = filepath.Join(home, ".timesheet-cli", "session.json")
	}
	client, err := api.New(api.Config{BaseURL: baseURL, SessionFile: sessionFile})
	if err != nil {
		return nil, classifyAPI(err)
	}
	return client, nil
}

func (a *app) success(data any, human func()) error {
	if a.json {
		return json.NewEncoder(a.out).Encode(map[string]any{"ok": true, "data": data})
	}
	human()
	return nil
}

func classify(err error) *commandError {
	var commandErr *commandError
	if errors.As(err, &commandErr) {
		return commandErr
	}
	return fail("usage", err.Error(), 2, err)
}

func classifyAPI(err error) *commandError {
	switch {
	case api.IsKind(err, api.KindAuth):
		return fail("auth_required", err.Error(), 3, err)
	case api.IsKind(err, api.KindLoginFailed):
		return fail("login_failed", err.Error(), 3, err)
	case api.IsKind(err, api.KindNetwork):
		return fail("network_error", err.Error(), 4, err)
	case api.IsKind(err, api.KindInvalidResponse):
		return fail("invalid_server_response", err.Error(), 5, err)
	case api.IsKind(err, api.KindOperation):
		return fail("operation_failed", err.Error(), 5, err)
	default:
		return fail("internal_error", err.Error(), 1, err)
	}
}

func fail(code, message string, exitCode int, cause error) *commandError {
	return &commandError{code: code, message: message, exitCode: exitCode, cause: cause}
}

func containsJSONFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || strings.HasPrefix(arg, "--json=") {
			return true
		}
	}
	return false
}
