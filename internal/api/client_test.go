package api_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gustmrg/timesheet-cli/internal/api"
)

const readPage = `<html><body><table id="tbWorksheet"><tbody><tr>
<td>ACME</td><td>Internal</td><td>Daily</td><td>20/07/2026</td><td>09:00</td><td>10:00</td><td>01:00</td>
<td><a id="123" name="Worksheet|456" title="Em análise">status</a></td><td><a id="123">edit</a></td>
</tr></tbody></table><select class="form-control customer" name="customer"><option value="10">ACME</option></select></body></html>`

func TestLoginFollowsRedirectsAndPersistsLegacyCookieMap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			http.SetCookie(w, &http.Cookie{Name: "csrf", Value: "landing", Path: "/"})
			io.WriteString(w, `<input name="__RequestVerificationToken" value="token">`)
		case r.Method == http.MethodPost && r.URL.Path == "/":
			if cookie, err := r.Cookie("csrf"); err != nil || cookie.Value != "landing" {
				t.Errorf("login cookie = %v, %v", cookie, err)
			}
			if r.FormValue("__RequestVerificationToken") != "token" || r.FormValue("Login") != "alice" || r.FormValue("Password") != "secret" {
				t.Errorf("unexpected login form: %#v", r.Form)
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "active", Path: "/"})
			http.Redirect(w, r, "/Worksheet/Read", http.StatusFound)
		case r.URL.Path == "/Worksheet/Read":
			if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "active" {
				t.Errorf("session cookie = %v, %v", cookie, err)
			}
			io.WriteString(w, readPage)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	session := filepath.Join(t.TempDir(), "nested", "session.json")
	client, err := api.New(api.Config{BaseURL: server.URL, SessionFile: session})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login("alice", "secret"); err != nil {
		t.Fatal(err)
	}
	entries, err := client.ListEntries()
	if err != nil || len(entries) != 1 || entries[0].ID != 123 {
		t.Fatalf("ListEntries() = %#v, %v", entries, err)
	}
	body, err := os.ReadFile(session)
	if err != nil {
		t.Fatal(err)
	}
	var cookies map[string]string
	if err := json.Unmarshal(body, &cookies); err != nil {
		t.Fatalf("session is not legacy JSON map: %v", err)
	}
	if cookies["session"] != "active" {
		t.Fatalf("saved cookies = %#v", cookies)
	}
	info, err := os.Stat(session)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("session mode = %v; want 0600", info.Mode().Perm())
	}
}

func TestMalformedSessionFailsAndFailedLoginDoesNotOverwriteIt(t *testing.T) {
	malformed := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(malformed, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := api.New(api.Config{BaseURL: "https://example.test", SessionFile: malformed}); err == nil {
		t.Fatal("New() succeeded with malformed session")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `<input name="__RequestVerificationToken" value="token">`)
			return
		}
		io.WriteString(w, `<input name="Login"><input name="Password">`)
	}))
	defer server.Close()
	session := filepath.Join(t.TempDir(), "session.json")
	original := []byte(`{"old":"still-valid"}`)
	if err := os.WriteFile(session, original, 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := api.New(api.Config{BaseURL: server.URL, SessionFile: session})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Login("wrong", "wrong"); !api.IsKind(err, api.KindLoginFailed) {
		t.Fatalf("Login() error = %v, want login failure", err)
	}
	after, _ := os.ReadFile(session)
	if string(after) != string(original) {
		t.Fatalf("failed login changed session: %q", after)
	}
}

func TestWorksheetEndpointsPreserveTheirWireContracts(t *testing.T) {
	seen := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path] = true
		if strings.HasPrefix(r.URL.Path, "/Worksheet/") && r.Header.Get("X-Requested-With") != "XMLHttpRequest" && r.URL.Path != "/Worksheet/Read" {
			t.Errorf("%s missing X-Requested-With", r.URL.Path)
		}
		switch r.URL.Path {
		case "/Worksheet/Read":
			io.WriteString(w, readPage)
		case "/Worksheet/DropDownChange":
			if r.URL.Query().Get("idcustomer") != "10" {
				t.Errorf("dropdown query = %q", r.URL.RawQuery)
			}
			writeJSON(w, []map[string]any{{"IdCustomer": 10, "CustomerName": "ACME", "IdProject": 20, "ProjectName": "Internal", "IdCategory": 30, "CategoryName": "Daily"}})
		case "/Worksheet/Update":
			writeJSON(w, map[string]any{"Id": 123, "IdCustomer": 10, "IdProject": 20, "IdCategory": 30, "InformedDate": "20/07/2026", "StartTime": "09:00", "EndTime": "10:00", "Description": "<p>Old</p>", "NotMonetize": false})
		case "/Worksheet/ReadEvaluate":
			if r.Method != http.MethodPost || r.FormValue("idworksheet") != "123" || r.FormValue("idevaluate") != "456" {
				t.Errorf("unexpected evaluate request")
			}
			writeJSON(w, map[string]any{"ManagerName": "Maria", "IsApprove": "1"})
		case "/Worksheet/UpdateMultiple":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"Description":"<p>&lt;b&gt;safe&lt;/b&gt;<br>next</p>"`) {
				t.Errorf("unsafe or unexpected create payload: %s", body)
			}
			writeJSON(w, map[string]any{"success": true, "createdWorksheets": []map[string]any{{"Id": 777}}})
		case "/Worksheet/UpdateOne":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"description":"<p>Old</p>"`) {
				t.Errorf("existing HTML was not preserved: %s", body)
			}
			writeJSON(w, map[string]any{"success": true})
		case "/Worksheet/Delete":
			if r.Method != http.MethodPost || r.FormValue("id") != "123" {
				t.Errorf("unexpected delete request")
			}
			writeJSON(w, map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := api.New(api.Config{BaseURL: server.URL, SessionFile: filepath.Join(t.TempDir(), "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Metadata(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DropDownChange(api.DropDownQuery{CustomerID: 10}); err != nil {
		t.Fatal(err)
	}
	worksheet, err := client.Worksheet(123)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Evaluate(123, 456); err != nil {
		t.Fatal(err)
	}
	input := api.EntryInput{Customer: api.Ref{ID: 10, Name: "ACME"}, Project: api.Ref{ID: 20, Name: "Internal"}, Category: api.Ref{ID: 30, Name: "Daily"}, Date: "20/07/2026", Start: "09:00", End: "10:00", Description: "<b>safe</b>\nnext"}
	created, err := client.Create(input)
	if err != nil || created.ID != 777 {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
	input.Description = worksheet.Description
	input.DescriptionHTML = true
	if err := client.Update(123, input); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(123); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/Worksheet/Read", "/Worksheet/DropDownChange", "/Worksheet/Update", "/Worksheet/ReadEvaluate", "/Worksheet/UpdateMultiple", "/Worksheet/UpdateOne", "/Worksheet/Delete"} {
		if !seen[path] {
			t.Errorf("endpoint %s was not called", path)
		}
	}
}

func TestExpiredSessionMalformedJSONAndRejectedWriteAreClassified(t *testing.T) {
	responses := map[string]string{
		"/Worksheet/Read":   `<input name="Login"><input name="Password">`,
		"/Worksheet/Update": `not-json`,
		"/Worksheet/Delete": `{"success":false,"message":"entry is locked"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, responses[r.URL.Path])
	}))
	defer server.Close()
	client, err := api.New(api.Config{BaseURL: server.URL, SessionFile: filepath.Join(t.TempDir(), "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListEntries(); !api.IsKind(err, api.KindAuth) {
		t.Fatalf("ListEntries() error = %v, want auth", err)
	}
	if _, err := client.Worksheet(123); !api.IsKind(err, api.KindInvalidResponse) {
		t.Fatalf("Worksheet() error = %v, want invalid response", err)
	}
	if err := client.Delete(123); !api.IsKind(err, api.KindOperation) || err.Error() != "entry is locked" {
		t.Fatalf("Delete() error = %v, want rejected operation", err)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
