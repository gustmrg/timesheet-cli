package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gustmrg/timesheet-cli/internal/credentials"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpTestToken = "test-token-0123456789-abcdefghijklmnop"

const mcpReadPage = `<html><body><table id="tbWorksheet"><tbody><tr>
<td>ACME</td><td>Internal</td><td>Daily</td><td>20/07/2026</td><td>09:00</td><td>10:00</td><td>01:00</td>
<td><a id="123" name="Worksheet|456" title="Em análise">status</a></td><td><a id="123">edit</a></td>
</tr></tbody></table><select class="form-control customer" name="customer"><option value="10">ACME</option></select></body></html>`

type recordedRequests struct {
	mu    sync.Mutex
	paths []string
	forms []string
}

func (r *recordedRequests) add(path, form string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, path)
	r.forms = append(r.forms, form)
}

func (r *recordedRequests) bodyFor(path string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, recorded := range r.paths {
		if recorded == path {
			return r.forms[index]
		}
	}
	return ""
}

func newMCPTimesheetServer(t *testing.T) (*httptest.Server, *recordedRequests) {
	t.Helper()
	requests := &recordedRequests{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests.add(r.URL.Path, string(body))
		writeJSON := func(value any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(value)
		}
		switch r.URL.Path {
		case "/Worksheet/Read":
			io.WriteString(w, mcpReadPage)
		case "/Worksheet/DropDownChange":
			writeJSON([]map[string]any{{"IdCustomer": 10, "CustomerName": "ACME", "IdProject": 20, "ProjectName": "Internal", "IdCategory": 30, "CategoryName": "Daily"}})
		case "/Worksheet/Update":
			writeJSON(map[string]any{"Id": 123, "IdCustomer": 10, "IdProject": 20, "IdCategory": 30, "InformedDate": "20/07/2026", "StartTime": "09:00", "EndTime": "10:00", "Description": "<p>Old</p>", "NotMonetize": false})
		case "/Worksheet/ReadEvaluate":
			writeJSON(map[string]any{"ManagerName": "Maria", "Created": "20/07/2026", "IsApprove": "1", "Description": "<p>Old notes</p>"})
		case "/Worksheet/UpdateMultiple":
			writeJSON(map[string]any{"success": true, "createdWorksheets": []map[string]any{{"Id": 777}}})
		case "/Worksheet/UpdateOne", "/Worksheet/Delete":
			writeJSON(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, requests
}

// connectMCP serves the tools over an in-memory transport. The credential store
// is a fake so tests never reach the operating system vault.
func connectMCP(t *testing.T) *mcp.ClientSession {
	t.Helper()
	a := &app{in: strings.NewReader(""), out: io.Discard, errOut: io.Discard, version: "test", credentialStore: &fakeCredentialStore{getErr: &credentials.Error{Kind: credentials.KindNotFound, Message: "no credentials"}}}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := a.newMCPServer().Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

type toolEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func callTool(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) (toolEnvelope, bool) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("CallTool(%s) returned %d content blocks", name, len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("CallTool(%s) returned %T", name, result.Content[0])
	}
	var envelope toolEnvelope
	if err := json.Unmarshal([]byte(text.Text), &envelope); err != nil {
		t.Fatalf("CallTool(%s) returned %q: %v", name, text.Text, err)
	}
	return envelope, result.IsError
}

func TestMCPListsEveryToolWithAccurateHints(t *testing.T) {
	session := connectMCP(t)
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	readOnly := map[string]bool{
		"list_entries": true, "get_metadata": true, "get_entry": true, "get_entry_status": true,
		"list_invoices": true, "preview_invoice": true,
		"create_entry": false, "update_entry": false, "delete_entry": false,
	}
	if len(result.Tools) != len(readOnly) {
		t.Fatalf("got %d tools, want %d", len(result.Tools), len(readOnly))
	}
	for _, tool := range result.Tools {
		want, known := readOnly[tool.Name]
		if !known {
			t.Errorf("unexpected tool %q", tool.Name)
			continue
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != want {
			t.Errorf("tool %q read-only hint = %+v, want %v", tool.Name, tool.Annotations, want)
		}
		if !want && (tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint != (tool.Name != "create_entry")) {
			t.Errorf("tool %q destructive hint = %v", tool.Name, tool.Annotations.DestructiveHint)
		}
	}
}

func TestMCPReadToolsReturnTheCLIEnvelope(t *testing.T) {
	server, _ := newMCPTimesheetServer(t)
	configureAuthTest(t, server.URL)
	session := connectMCP(t)

	for _, test := range []struct {
		tool      string
		arguments map[string]any
		contains  []string
	}{
		{"list_entries", map[string]any{"limit": 1}, []string{`"id":123`, `"returned":1`, `"total":1`}},
		{"get_metadata", nil, []string{`"customers"`, `"id":10`, `"id":20`, `"id":30`}},
		{"get_entry", map[string]any{"entryId": 123}, []string{`"entryId":123`, `"description":"Old notes"`}},
		{"get_entry_status", map[string]any{"entryId": 123}, []string{`"state":"approved"`, `"manager":"Maria"`}},
	} {
		envelope, isError := callTool(t, session, test.tool, test.arguments)
		if isError || !envelope.OK {
			t.Fatalf("%s failed: %+v", test.tool, envelope)
		}
		for _, fragment := range test.contains {
			if !strings.Contains(string(envelope.Data), fragment) {
				t.Errorf("%s data %s missing %s", test.tool, envelope.Data, fragment)
			}
		}
	}
}

func TestMCPWriteToolsCreateUpdateAndDelete(t *testing.T) {
	server, requests := newMCPTimesheetServer(t)
	configureAuthTest(t, server.URL)
	session := connectMCP(t)

	envelope, isError := callTool(t, session, "create_entry", map[string]any{
		"customer": "acme", "project": "internal", "category": "daily",
		"date": "2026-07-20", "start": "09:00", "end": "10:00", "description": "Implementação de funcionalidade X.",
	})
	if isError || !strings.Contains(string(envelope.Data), `"action":"created"`) || !strings.Contains(string(envelope.Data), `"id":777`) {
		t.Fatalf("create_entry = %+v", envelope)
	}

	envelope, isError = callTool(t, session, "update_entry", map[string]any{"entryId": 123, "end": "11:00", "monetized": false})
	if isError || !strings.Contains(string(envelope.Data), `"notMonetized":true`) || !strings.Contains(string(envelope.Data), `"end":"11:00"`) {
		t.Fatalf("update_entry = %+v", envelope)
	}
	if body := requests.bodyFor("/Worksheet/UpdateOne"); !strings.Contains(body, "Old") {
		t.Errorf("update without a description should keep the current one, sent %q", body)
	}

	envelope, isError = callTool(t, session, "delete_entry", map[string]any{"entryId": 123})
	if isError || !strings.Contains(string(envelope.Data), `"deleted":true`) {
		t.Fatalf("delete_entry = %+v", envelope)
	}
}

func TestMCPFailuresUseStableErrorCodes(t *testing.T) {
	server, _ := newMCPTimesheetServer(t)
	configureAuthTest(t, server.URL)
	session := connectMCP(t)

	for _, test := range []struct {
		tool      string
		arguments map[string]any
		code      string
	}{
		{"get_entry", map[string]any{"entryId": 0}, "invalid_input"},
		{"get_entry", map[string]any{"entryId": 999}, "not_found"},
		{"list_invoices", map[string]any{"status": "lost"}, "invalid_input"},
		{"create_entry", map[string]any{"customer": "nobody", "project": "x", "category": "y", "start": "09:00", "end": "10:00", "description": "x"}, "invalid_input"},
		{"update_entry", map[string]any{"entryId": 123, "start": "9h"}, "invalid_input"},
	} {
		envelope, isError := callTool(t, session, test.tool, test.arguments)
		if !isError || envelope.OK || envelope.Error.Code != test.code {
			t.Errorf("%s(%v) = isError %v, %+v; want code %s", test.tool, test.arguments, isError, envelope, test.code)
		}
	}
}

func TestMCPHTTPRequiresTheBearerToken(t *testing.T) {
	timesheet, _ := newMCPTimesheetServer(t)
	configureAuthTest(t, timesheet.URL)
	a := &app{in: strings.NewReader(""), out: io.Discard, errOut: io.Discard, version: "test", credentialStore: &fakeCredentialStore{getErr: &credentials.Error{Kind: credentials.KindNotFound, Message: "no credentials"}}}
	server := a.newMCPServer()
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{DisableLocalhostProtection: true})
	endpoint := httptest.NewServer(requireBearer(mcpTestToken, handler))
	t.Cleanup(endpoint.Close)

	for _, header := range []string{"", "Bearer wrong", mcpTestToken} {
		request, err := http.NewRequest(http.MethodPost, endpoint.URL, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("Authorization %q got status %d, want 401", header, response.StatusCode)
		}
	}

	transport := &mcp.StreamableClientTransport{Endpoint: endpoint.URL, HTTPClient: &http.Client{Transport: bearerTransport{token: mcpTestToken}}, MaxRetries: -1}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil).Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	envelope, isError := callTool(t, session, "list_entries", nil)
	if isError || !envelope.OK || !strings.Contains(string(envelope.Data), `"id":123`) {
		t.Fatalf("list_entries over HTTP = %+v", envelope)
	}
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(request)
}

func TestMCPTokenIsGeneratedOnceAndCanBeOverridden(t *testing.T) {
	t.Setenv("TIMESHEET_MCP_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	a := &app{errOut: io.Discard}

	first, err := a.mcpToken("", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < mcpMinTokenLength {
		t.Fatalf("generated token is %d characters", len(first))
	}
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".timesheet-cli", "mcp-token")
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("token file mode = %v, want 0600", info.Mode().Perm())
	}
	second, err := a.mcpToken("", false)
	if err != nil || second != first {
		t.Fatalf("second read = %q, %v; want the saved token", second, err)
	}

	if _, err := a.mcpToken(filepath.Join(t.TempDir(), "missing"), true); err == nil {
		t.Error("an explicit token file that does not exist must be an error")
	}

	t.Setenv("TIMESHEET_MCP_TOKEN", mcpTestToken)
	if token, err := a.mcpToken("", false); err != nil || token != mcpTestToken {
		t.Errorf("environment token = %q, %v", token, err)
	}
	t.Setenv("TIMESHEET_MCP_TOKEN", "short")
	if _, err := a.mcpToken("", false); err == nil {
		t.Error("a short environment token must be rejected")
	}
}
