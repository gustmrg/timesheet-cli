package cli

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

const (
	mcpEndpointPath    = "/mcp"
	mcpMinTokenLength  = 32
	mcpSessionTimeout  = 30 * time.Minute
	mcpShutdownTimeout = 5 * time.Second
)

const mcpInstructions = `Manage the user's Luby Timesheet entries and inspect invoice processes.

- Write every new or edited entry description in Brazilian Portuguese (pt-BR), even when the request is in another language. Use an objective, impersonal construction built on action nouns ("Implementação de…", "Correção de…", "Análise de…"), never first person. Prefer short prose paragraphs over bullets. Translate the supplied activity faithfully without inventing work.
- Customer, project, and category accept a numeric ID or a unique, case-insensitive name substring. When a value is missing or ambiguous, call get_metadata and ask the user to choose; never guess.
- Dates use YYYY-MM-DD or dd/MM/yyyy and default to today. Times use 24-hour HH:mm.
- Before deleting, show the user the entry's ID, date, time, project, and category. Never infer deletion from a general cleanup request.
- Writes are never retried automatically. After auth_required on a write or an uncertain network failure, list the entries to see whether the write happened before calling the tool again.
- Successful results are {"ok":true,"data":...} and may carry "warnings"; failures are {"ok":false,"error":{"code":"...","message":"..."}}.`

type listEntriesInput struct {
	Limit int  `json:"limit,omitempty" jsonschema:"maximum number of entries to return; defaults to 20"`
	All   bool `json:"all,omitempty" jsonschema:"return every entry and ignore limit"`
}

type entryIDInput struct {
	EntryID int64 `json:"entryId" jsonschema:"the entry ID, as returned by list_entries"`
}

type createEntryInput struct {
	Customer     string `json:"customer" jsonschema:"customer ID or unique name substring"`
	Project      string `json:"project" jsonschema:"project ID or unique name substring"`
	Category     string `json:"category" jsonschema:"category ID or unique name substring"`
	Date         string `json:"date,omitempty" jsonschema:"date as YYYY-MM-DD or dd/MM/yyyy; defaults to today"`
	Start        string `json:"start" jsonschema:"start time as 24-hour HH:mm"`
	End          string `json:"end" jsonschema:"end time as 24-hour HH:mm"`
	Description  string `json:"description" jsonschema:"plain-text description in Brazilian Portuguese"`
	NotMonetized bool   `json:"notMonetized,omitempty" jsonschema:"true only when the user says the entry is not monetized"`
}

type updateEntryInput struct {
	EntryID     int64  `json:"entryId" jsonschema:"the entry ID, as returned by list_entries"`
	Customer    string `json:"customer,omitempty" jsonschema:"new customer ID or unique name substring"`
	Project     string `json:"project,omitempty" jsonschema:"new project ID or unique name substring"`
	Category    string `json:"category,omitempty" jsonschema:"new category ID or unique name substring"`
	Date        string `json:"date,omitempty" jsonschema:"new date as YYYY-MM-DD or dd/MM/yyyy"`
	Start       string `json:"start,omitempty" jsonschema:"new start time as 24-hour HH:mm"`
	End         string `json:"end,omitempty" jsonschema:"new end time as 24-hour HH:mm"`
	Description string `json:"description,omitempty" jsonschema:"new plain-text description in Brazilian Portuguese"`
	Monetized   *bool  `json:"monetized,omitempty" jsonschema:"true marks the entry monetized, false marks it not monetized; omit to keep the current value"`
}

type listInvoicesInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum number of invoice processes to return; defaults to 20"`
	Offset int    `json:"offset,omitempty" jsonschema:"number of matching invoice processes to skip"`
	Search string `json:"search,omitempty" jsonschema:"text to search invoice records for"`
	Status string `json:"status,omitempty" jsonschema:"pending or sent"`
	All    bool   `json:"all,omitempty" jsonschema:"return every matching invoice process and ignore limit and offset"`
}

type invoiceProcessInput struct {
	ProcessID int64 `json:"processId" jsonschema:"the invoice process ID, as returned by list_invoices"`
}

func (a *app) mcpCommand() *cobra.Command {
	var listen, tokenFile string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve timesheet tools over the Model Context Protocol",
		Long: `Serve timesheet tools to AI agents over the Model Context Protocol.

Without --http the server speaks MCP over stdin and stdout, for clients that
start it as a local process. With --http it serves Streamable HTTP at /mcp on
the given address and requires every request to carry
"Authorization: Bearer <token>". The token comes from TIMESHEET_MCP_TOKEN or
from the token file, which is created with a random token on first use.

The server authenticates with the session and saved credentials of
"timesheet login --save-credentials"; agents never see them.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			server := a.newMCPServer()
			if listen == "" {
				if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
					return fail("operation_failed", "MCP server stopped: "+err.Error(), 1, err)
				}
				return nil
			}
			token, err := a.mcpToken(tokenFile, cmd.Flags().Changed("token-file"))
			if err != nil {
				return err
			}
			return serveMCPHTTP(ctx, listen, token, server, a.errOut)
		},
	}
	cmd.AddCommand(a.mcpInstallCommand(), a.mcpUninstallCommand())
	cmd.Flags().StringVar(&listen, "http", "", "serve Streamable HTTP on this address, such as 127.0.0.1:8765")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "bearer token file (default ~/.timesheet-cli/mcp-token)")
	return cmd
}

// mcpTools serializes tool calls: they share one session file, and a renewal
// running in parallel with another call would log in twice.
type mcpTools struct {
	mu     sync.Mutex
	newApp func() *app
}

func (a *app) newMCPServer() *mcp.Server {
	tools := &mcpTools{newApp: func() *app {
		return &app{in: strings.NewReader(""), out: io.Discard, errOut: io.Discard, json: true, version: a.version, credentialStore: a.credentialStore}
	}}
	server := mcp.NewServer(&mcp.Implementation{Name: "timesheet", Title: "Luby Timesheet", Version: a.version}, &mcp.ServerOptions{Instructions: mcpInstructions})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPointer(true)}

	mcp.AddTool(server, &mcp.Tool{Name: "list_entries", Title: "List entries", Annotations: readOnly,
		Description: "List the user's timesheet entries with their IDs, dates, times, customer, project, category, and approval status. Descriptions are not included; use get_entry for one.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input listEntriesInput) (*mcp.CallToolResult, any, error) {
		limit := input.Limit
		if limit == 0 {
			limit = 20
		}
		return runMCPTool(tools, func(a *app) (any, error) { return a.listEntries(limit, input.All) })
	})

	mcp.AddTool(server, &mcp.Tool{Name: "get_metadata", Title: "List customers, projects, and categories", Annotations: readOnly,
		Description: "List the customers available to the user, each with its projects and each project's categories, with their IDs. Use it to resolve missing or ambiguous names before creating or updating an entry.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return runMCPTool(tools, func(a *app) (any, error) {
			customers, err := a.metadata()
			return map[string]any{"customers": customers}, err
		})
	})

	mcp.AddTool(server, &mcp.Tool{Name: "get_entry", Title: "Show entry", Annotations: readOnly,
		Description: "Show one timesheet entry including its description.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input entryIDInput) (*mcp.CallToolResult, any, error) {
		return runMCPTool(tools, func(a *app) (any, error) {
			id, err := mcpID("entry", input.EntryID)
			if err != nil {
				return nil, err
			}
			return a.entryDetails(id)
		})
	})

	mcp.AddTool(server, &mcp.Tool{Name: "get_entry_status", Title: "Show entry evaluation", Annotations: readOnly,
		Description: "Show an entry's evaluation: pending, in_review, pre_approved, pre_rejected, approved, or rejected, with the manager and the evaluation date.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input entryIDInput) (*mcp.CallToolResult, any, error) {
		return runMCPTool(tools, func(a *app) (any, error) {
			id, err := mcpID("entry", input.EntryID)
			if err != nil {
				return nil, err
			}
			status, _, err := a.entryStatus(id)
			return status, err
		})
	})

	mcp.AddTool(server, &mcp.Tool{Name: "list_invoices", Title: "List invoice processes", Annotations: readOnly,
		Description: "List invoice processes with their process IDs, cut dates, hours, amounts, and pending or sent status.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input listInvoicesInput) (*mcp.CallToolResult, any, error) {
		limit := input.Limit
		if limit == 0 {
			limit = 20
		}
		return runMCPTool(tools, func(a *app) (any, error) {
			return a.listInvoices(invoiceListOptions{limit: limit, offset: input.Offset, search: input.Search, status: input.Status, all: input.All})
		})
	})

	mcp.AddTool(server, &mcp.Tool{Name: "preview_invoice", Title: "Preview invoice process", Annotations: readOnly,
		Description: "Show one invoice process broken down by customer and project, with monetized and non-monetized hours and amounts.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input invoiceProcessInput) (*mcp.CallToolResult, any, error) {
		return runMCPTool(tools, func(a *app) (any, error) {
			id, err := mcpID("process", input.ProcessID)
			if err != nil {
				return nil, err
			}
			return a.invoicePreview(id)
		})
	})

	mcp.AddTool(server, &mcp.Tool{Name: "create_entry", Title: "Create entry",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(false), OpenWorldHint: boolPointer(true)},
		Description: "Create a timesheet entry. Write the description in Brazilian Portuguese with an impersonal construction, such as \"Implementação de funcionalidade X.\" Returns the created entry with its ID.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input createEntryInput) (*mcp.CallToolResult, any, error) {
		return runMCPTool(tools, func(a *app) (any, error) {
			return a.createEntry(entryFlags{
				customer: input.Customer, project: input.Project, category: input.Category,
				date: input.Date, start: input.Start, end: input.End, description: input.Description,
				notMonetize: input.NotMonetized,
			})
		})
	})

	mcp.AddTool(server, &mcp.Tool{Name: "update_entry", Title: "Update entry", InputSchema: updateEntrySchema(),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(true), IdempotentHint: true, OpenWorldHint: boolPointer(true)},
		Description: "Change a timesheet entry. Only the fields given change; the others keep their current values. A new description follows the same Brazilian Portuguese rules as create_entry.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input updateEntryInput) (*mcp.CallToolResult, any, error) {
		return runMCPTool(tools, func(a *app) (any, error) {
			id, err := mcpID("entry", input.EntryID)
			if err != nil {
				return nil, err
			}
			flags := entryFlags{
				customer: input.Customer, project: input.Project, category: input.Category,
				date: input.Date, start: input.Start, end: input.End, description: input.Description,
			}
			if input.Monetized != nil {
				flags.monetize = *input.Monetized
				flags.notMonetize = !*input.Monetized
			}
			return a.updateEntry(id, flags)
		})
	})

	mcp.AddTool(server, &mcp.Tool{Name: "delete_entry", Title: "Delete entry",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(true), IdempotentHint: true, OpenWorldHint: boolPointer(true)},
		Description: "Permanently delete a timesheet entry. Only call it after the user confirmed deleting this exact entry.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, input entryIDInput) (*mcp.CallToolResult, any, error) {
		return runMCPTool(tools, func(a *app) (any, error) {
			id, err := mcpID("entry", input.EntryID)
			if err != nil {
				return nil, err
			}
			return a.deleteEntry(id)
		})
	})

	return server
}

// runMCPTool runs one operation on a fresh app and wraps the outcome in the
// same envelope the CLI prints with --json.
func runMCPTool(tools *mcpTools, operation func(*app) (any, error)) (*mcp.CallToolResult, any, error) {
	tools.mu.Lock()
	defer tools.mu.Unlock()
	a := tools.newApp()
	data, err := operation(a)
	if err != nil {
		commandErr := classify(err)
		body, marshalErr := json.Marshal(map[string]any{
			"ok":    false,
			"error": map[string]string{"code": commandErr.code, "message": commandErr.message},
		})
		if marshalErr != nil {
			return nil, nil, marshalErr
		}
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
	envelope := map[string]any{"ok": true, "data": data}
	if warnings := a.collectWarnings(); len(warnings) != 0 {
		envelope["warnings"] = warnings
	}
	return nil, envelope, nil
}

func mcpID(kind string, id int64) (int64, error) {
	if id <= 0 {
		return 0, fail("invalid_input", fmt.Sprintf("invalid %s ID %d", kind, id), 2, nil)
	}
	return id, nil
}

func boolPointer(value bool) *bool { return &value }

// updateEntrySchema declares monetized as a plain boolean. Reflection would
// make the optional *bool nullable, a type union some model providers reject.
func updateEntrySchema() *jsonschema.Schema {
	schema, err := jsonschema.For[updateEntryInput](nil)
	if err != nil {
		panic(fmt.Sprintf("update_entry schema: %v", err))
	}
	monetized := schema.Properties["monetized"]
	monetized.Type, monetized.Types = "boolean", nil
	return schema
}

// mcpToken returns the bearer token for the HTTP transport, creating the
// default token file on first use. An explicitly named file must already exist.
func (a *app) mcpToken(path string, explicit bool) (string, error) {
	if value := strings.TrimSpace(os.Getenv("TIMESHEET_MCP_TOKEN")); value != "" {
		if len(value) < mcpMinTokenLength {
			return "", fail("invalid_input", fmt.Sprintf("TIMESHEET_MCP_TOKEN must be at least %d characters", mcpMinTokenLength), 2, nil)
		}
		return value, nil
	}
	return a.mcpTokenFile(path, explicit)
}

// mcpTokenFile returns the token saved in path, or in the default token file,
// creating the default file with a random token when it does not exist yet.
func (a *app) mcpTokenFile(path string, explicit bool) (string, error) {
	if path == "" {
		var err error
		if path, err = defaultMCPTokenPath(); err != nil {
			return "", err
		}
	}
	body, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(body))
		if len(token) < mcpMinTokenLength {
			return "", fail("invalid_input", fmt.Sprintf("the token in %s must be at least %d characters", path, mcpMinTokenLength), 2, nil)
		}
		return token, nil
	}
	if !errors.Is(err, os.ErrNotExist) || explicit {
		return "", fail("invalid_input", "read MCP token file "+path, 2, err)
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fail("internal_error", "generate MCP token", 1, err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fail("internal_error", "create MCP token directory", 1, err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fail("internal_error", "create MCP token file "+path, 1, err)
	}
	_, writeErr := file.WriteString(token + "\n")
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(path)
		return "", fail("internal_error", "write MCP token file "+path, 1, err)
	}
	fmt.Fprintf(a.errOut, "generated a bearer token in %s\n", path)
	return token, nil
}

func defaultMCPTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fail("internal_error", "determine home directory", 1, err)
	}
	return filepath.Join(home, ".timesheet-cli", "mcp-token"), nil
}

func serveMCPHTTP(ctx context.Context, address, token string, server *mcp.Server, log io.Writer) error {
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		SessionTimeout: mcpSessionTimeout,
		// Reverse proxies such as `tailscale serve` reach the loopback listener
		// with their public Host header, which the SDK's DNS-rebinding check
		// rejects. The bearer token already stops requests a hostile page could
		// make through a rebound name.
		DisableLocalhostProtection: true,
	})
	mux := http.NewServeMux()
	mux.Handle(mcpEndpointPath, requireBearer(token, handler))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fail("invalid_input", "listen on "+address, 2, err)
	}
	httpServer := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), mcpShutdownTimeout)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	fmt.Fprintf(log, "timesheet MCP server listening on http://%s%s\n", listener.Addr(), mcpEndpointPath)
	if err := httpServer.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return fail("operation_failed", "MCP server stopped: "+err.Error(), 1, err)
	}
	<-shutdownDone
	return nil
}

func requireBearer(token string, next http.Handler) http.Handler {
	expected := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), expected) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="timesheet"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
