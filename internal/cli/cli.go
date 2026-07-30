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
	"github.com/gustmrg/timesheet-cli/internal/credentials"
	"github.com/spf13/cobra"
)

const defaultBaseURL = "https://luby-timesheet.azurewebsites.net"

type app struct {
	in              io.Reader
	out             io.Writer
	errOut          io.Writer
	json            bool
	version         string
	credentialStore credentials.Store
	clients         []*api.Client
	warnings        []api.Warning
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
	return executeWithStore(args, in, out, errOut, version, credentials.NewKeyringStore())
}

func executeWithStore(args []string, in io.Reader, out, errOut io.Writer, version string, store credentials.Store) int {
	a := &app{in: in, out: out, errOut: errOut, version: version, credentialStore: store}
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
		a.loginCommand(), a.logoutCommand(), a.listCommand(), a.metaCommand(), a.statusCommand(),
		a.addCommand(), a.updateCommand(), a.deleteCommand(), a.versionCommand(),
	)
	return root
}

func (a *app) client() (*api.Client, error) {
	baseURL := a.baseURL()
	sessionFile, err := a.sessionFile()
	if err != nil {
		return nil, err
	}
	client, err := api.New(api.Config{
		BaseURL:     baseURL,
		SessionFile: sessionFile,
		RenewSession: func(client *api.Client) error {
			return a.renewSession(client, baseURL)
		},
	})
	if err != nil {
		return nil, classifyAPI(err)
	}
	a.clients = append(a.clients, client)
	return client, nil
}

func (a *app) success(data any, human func()) error {
	warnings := append([]api.Warning(nil), a.warnings...)
	for _, client := range a.clients {
		for _, warning := range client.Warnings() {
			warnings = appendWarning(warnings, warning)
		}
	}
	if a.json {
		envelope := map[string]any{"ok": true, "data": data}
		if len(warnings) != 0 {
			envelope["warnings"] = warnings
		}
		return json.NewEncoder(a.out).Encode(envelope)
	}
	human()
	for _, warning := range warnings {
		fmt.Fprintf(a.errOut, "warning: %s\n", warning.Message)
	}
	return nil
}

func (a *app) addWarning(code, message string) {
	a.warnings = appendWarning(a.warnings, api.Warning{Code: code, Message: message})
}

func appendWarning(warnings []api.Warning, warning api.Warning) []api.Warning {
	for _, existing := range warnings {
		if existing.Code == warning.Code {
			return warnings
		}
	}
	return append(warnings, warning)
}

func (a *app) baseURL() string {
	if value := os.Getenv("TIMESHEET_BASE_URL"); value != "" {
		return value
	}
	return defaultBaseURL
}

func (a *app) sessionFile() (string, error) {
	if value := os.Getenv("TIMESHEET_SESSION"); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fail("internal_error", "determine home directory", 1, err)
	}
	return filepath.Join(home, ".timesheet-cli", "session.json"), nil
}

func (a *app) credentialsFile() (string, error) {
	if value := os.Getenv("TIMESHEET_CREDENTIALS"); value != "" {
		return value, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fail("internal_error", "determine home directory", 1, err)
	}
	return filepath.Join(home, ".timesheet-cli", "credentials.json"), nil
}

func (a *app) fileCredentialStore() (*credentials.FileStore, string, error) {
	path, err := a.credentialsFile()
	if err != nil {
		return nil, "", err
	}
	return credentials.NewFileStore(path), path, nil
}

func (a *app) renewSession(client *api.Client, baseURL string) error {
	user := os.Getenv("TIMESHEET_USER")
	password := os.Getenv("TIMESHEET_PASS")
	var credentialSource credentials.Store
	if user == "" || password == "" {
		record, storeErr := a.credentialStore.Get(baseURL)
		credentialSource = a.credentialStore
		if storeErr != nil {
			fileStore, _, fileStoreErr := a.fileCredentialStore()
			if fileStoreErr != nil {
				return fileStoreErr
			}
			record, storeErr = fileStore.Get(baseURL)
			credentialSource = fileStore
		}
		if storeErr != nil {
			switch {
			case credentials.IsKind(storeErr, credentials.KindCorrupt):
				return &api.Error{Kind: api.KindAuth, Message: "session expired and saved credentials are invalid; run `timesheet login --save-credentials --credential-store file`"}
			case credentials.IsKind(storeErr, credentials.KindStore):
				return &api.Error{Kind: api.KindAuth, Message: "session expired and the credentials file is unavailable; check its ownership and permissions or run `timesheet login`"}
			default:
				return &api.Error{Kind: api.KindAuth, Message: "session expired and no saved credentials are available; run `timesheet login --save-credentials --credential-store file`"}
			}
		}
		user, password = record.Username, record.Password
	}
	if err := client.Login(user, password); err != nil {
		if credentialSource != nil && api.IsKind(err, api.KindLoginFailed) {
			message := "saved credentials were rejected and removed; run `timesheet login --save-credentials`"
			if deleteErr := credentialSource.Delete(baseURL); deleteErr != nil {
				message = "saved credentials were rejected and could not be removed; remove them manually, then run `timesheet login --save-credentials`"
			}
			return &api.Error{Kind: api.KindLoginFailed, Message: message}
		}
		return err
	}
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
