package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gustmrg/timesheet-cli/internal/cli"
)

const cliReadPage = `<html><body><table id="tbWorksheet"><tbody><tr>
<td>ACME</td><td>Internal</td><td>Daily</td><td>20/07/2026</td><td>09:00</td><td>10:00</td><td>01:00</td>
<td><a id="123" name="Worksheet|456" title="Em análise">status</a></td><td><a id="123">edit</a></td>
</tr></tbody></table><select class="form-control customer" name="customer"><option value="10">ACME</option></select></body></html>`

func TestVersionAndCommandHelpUseStandardCLIForms(t *testing.T) {
	for _, test := range []struct {
		args     []string
		contains string
	}{
		{[]string{"--version"}, "timesheet version 0.2.0-test"},
		{[]string{"version", "--json"}, `"version":"0.2.0-test"`},
		{[]string{"add", "--help"}, "--customer"},
		{[]string{"-h"}, "Available Commands"},
	} {
		code, stdout, stderr := execute(t, test.args, "")
		if code != 0 || stderr != "" || !strings.Contains(stdout, test.contains) {
			t.Fatalf("Execute(%v) = code %d, stdout %q, stderr %q", test.args, code, stdout, stderr)
		}
	}
}

func TestListProducesNormalizedJSONEnvelopeAndHumanTable(t *testing.T) {
	server := newCLIServer(t)
	defer server.Close()
	configure(t, server.URL)

	code, stdout, stderr := execute(t, []string{"--json", "list", "--limit", "1"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Entries []struct {
				ID int64 `json:"id"`
			} `json:"entries"`
			Returned, Total int
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.Returned != 1 || envelope.Data.Total != 1 || envelope.Data.Entries[0].ID != 123 {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}

	code, stdout, stderr = execute(t, []string{"list"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "ID") || !strings.Contains(stdout, "123") {
		t.Fatalf("human list = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestLoginProducesJSONAndSavesTheConfiguredSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.SetCookie(w, &http.Cookie{Name: "csrf", Value: "token-cookie", Path: "/"})
			io.WriteString(w, `<input name="__RequestVerificationToken" value="token">`)
			return
		}
		if r.FormValue("Login") != "alice" || r.FormValue("Password") != "secret" {
			t.Errorf("unexpected credentials")
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "active", Path: "/"})
		io.WriteString(w, `<html>authenticated</html>`)
	}))
	defer server.Close()
	session := filepath.Join(t.TempDir(), "session.json")
	t.Setenv("TIMESHEET_BASE_URL", server.URL)
	t.Setenv("TIMESHEET_SESSION", session)
	code, stdout, stderr := execute(t, []string{"login", "--user", "alice", "--pass", "secret", "--json"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"authenticated":true`) || !strings.Contains(stdout, session) {
		t.Fatalf("login = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestValidationErrorsUseStableJSONCodesAndStderr(t *testing.T) {
	configure(t, "https://example.invalid")
	for _, args := range [][]string{
		{"list", "--limit", "0", "--json"},
		{"status", "not-an-id", "--json"},
		{"add", "--json"},
		{"add", "--customer", "10", "--project", "20", "--category", "30", "--date", "2026-02-30", "--start", "09:00", "--end", "10:00", "--desc", "x", "--json"},
		{"update", "123", "--desc", "", "--json"},
		{"update", "123", "--monetize", "--not-monetize", "--json"},
	} {
		code, stdout, stderr := execute(t, args, "")
		if code != 2 || stdout != "" || !strings.Contains(stderr, `"ok":false`) || !strings.Contains(stderr, `"code":"invalid_input"`) {
			t.Fatalf("Execute(%v) = code %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
}

func TestReadAndWriteCommandsExposeStableJSONData(t *testing.T) {
	server := newCLIServer(t)
	defer server.Close()
	configure(t, server.URL)

	tests := []struct {
		args     []string
		contains []string
	}{
		{[]string{"meta", "--json"}, []string{`"customers"`, `"id":10`, `"id":20`, `"id":30`}},
		{[]string{"status", "123", "--json"}, []string{`"state":"approved"`, `"manager":"Maria"`}},
		{[]string{"add", "--customer", "acme", "--project", "internal", "--category", "daily", "--date", "2026-07-20", "--start", "09:00", "--end", "10:00", "--desc", "Work", "--json"}, []string{`"action":"created"`, `"id":777`}},
		{[]string{"update", "123", "--monetize", "--end", "11:00", "--json"}, []string{`"action":"updated"`, `"notMonetized":false`}},
		{[]string{"delete", "123", "--yes", "--json"}, []string{`"deleted":true`, `"id":123`}},
	}
	for _, test := range tests {
		code, stdout, stderr := execute(t, test.args, "")
		if code != 0 || stderr != "" {
			t.Fatalf("Execute(%v) = code %d, stdout %q, stderr %q", test.args, code, stdout, stderr)
		}
		for _, fragment := range test.contains {
			if !strings.Contains(stdout, fragment) {
				t.Errorf("Execute(%v) output %q missing %q", test.args, stdout, fragment)
			}
		}
	}
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	server := newCLIServer(t)
	defer server.Close()
	configure(t, server.URL)
	code, stdout, stderr := execute(t, []string{"delete", "123"}, "n\n")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "aborted") {
		t.Fatalf("delete refusal = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestEveryEvaluationFlagHasANormalizedState(t *testing.T) {
	tests := []struct{ field, state string }{
		{"IsWait", "pending"}, {"IsApprove", "approved"}, {"IsReprove", "rejected"},
		{"IsPreApproved", "pre_approved"}, {"IsPreReproved", "pre_rejected"}, {"IsReview", "in_review"},
	}
	for _, test := range tests {
		t.Run(test.state, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/Worksheet/Read" {
					io.WriteString(w, cliReadPage)
					return
				}
				writeCLIJSON(w, map[string]any{test.field: "1"})
			}))
			defer server.Close()
			configure(t, server.URL)
			code, stdout, stderr := execute(t, []string{"status", "123", "--json"}, "")
			if code != 0 || stderr != "" || !strings.Contains(stdout, `"state":"`+test.state+`"`) {
				t.Fatalf("status = code %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}
}

func TestAmbiguousMetadataNameUsesStableErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `<select class="form-control customer" name="customer"><option value="10">ACME Brazil</option><option value="11">ACME Europe</option></select>`)
	}))
	defer server.Close()
	configure(t, server.URL)
	args := []string{"add", "--customer", "acme", "--project", "20", "--category", "30", "--start", "09:00", "--end", "10:00", "--desc", "Work", "--json"}
	code, stdout, stderr := execute(t, args, "")
	if code != 2 || stdout != "" || !strings.Contains(stderr, `"code":"ambiguous_value"`) {
		t.Fatalf("ambiguous add = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func execute(t *testing.T, args []string, stdin string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Execute(args, strings.NewReader(stdin), &stdout, &stderr, "0.2.0-test")
	return code, stdout.String(), stderr.String()
}

func configure(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("TIMESHEET_BASE_URL", baseURL)
	t.Setenv("TIMESHEET_SESSION", filepath.Join(t.TempDir(), "session.json"))
}

func newCLIServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Worksheet/Read":
			io.WriteString(w, cliReadPage)
		case "/Worksheet/DropDownChange":
			if r.URL.Query().Get("idcustomer") != "" {
				writeCLIJSON(w, []map[string]any{{"IdCustomer": 10, "CustomerName": "ACME", "IdProject": 20, "ProjectName": "Internal", "IdCategory": 30, "CategoryName": "Daily"}})
			} else {
				writeCLIJSON(w, []map[string]any{{"IdProject": 20, "ProjectName": "Internal", "IdCategory": 30, "CategoryName": "Daily"}})
			}
		case "/Worksheet/Update":
			writeCLIJSON(w, map[string]any{"Id": 123, "IdCustomer": 10, "IdProject": 20, "IdCategory": 30, "InformedDate": "20/07/2026", "StartTime": "09:00", "EndTime": "10:00", "Description": "<p>Old</p>", "NotMonetize": true})
		case "/Worksheet/ReadEvaluate":
			writeCLIJSON(w, map[string]any{"ManagerName": "Maria", "Created": "20/07/2026", "IsApprove": "1"})
		case "/Worksheet/UpdateMultiple":
			writeCLIJSON(w, map[string]any{"success": true, "createdWorksheets": []map[string]any{{"Id": 777}}})
		case "/Worksheet/UpdateOne", "/Worksheet/Delete":
			writeCLIJSON(w, map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeCLIJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(fmt.Sprintf("encode fixture: %v", err))
	}
}
