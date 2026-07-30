package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gustmrg/timesheet-cli/internal/credentials"
)

type fakeCredentialStore struct {
	record      credentials.Record
	getErr      error
	setErr      error
	deleteErr   error
	getCalls    int
	setCalls    int
	deleteCalls int
}

func (f *fakeCredentialStore) Get(string) (credentials.Record, error) {
	f.getCalls++
	if f.getErr != nil {
		return credentials.Record{}, f.getErr
	}
	return f.record, nil
}
func (f *fakeCredentialStore) Set(_ string, value credentials.Record) error {
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	f.record = value
	return nil
}
func (f *fakeCredentialStore) Delete(string) error {
	f.deleteCalls++
	return f.deleteErr
}

func runWithStore(t *testing.T, store credentials.Store, args []string, stdin string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := executeWithStore(args, strings.NewReader(stdin), &stdout, &stderr, "test", store)
	return code, stdout.String(), stderr.String()
}

func configureAuthTest(t *testing.T, baseURL string) string {
	t.Helper()
	directory := t.TempDir()
	session := filepath.Join(directory, "session.json")
	t.Setenv("TIMESHEET_BASE_URL", baseURL)
	t.Setenv("TIMESHEET_SESSION", session)
	t.Setenv("TIMESHEET_CREDENTIALS", filepath.Join(directory, "credentials.json"))
	t.Setenv("TIMESHEET_USER", "")
	t.Setenv("TIMESHEET_PASS", "")
	return session
}

func loginServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.SetCookie(w, &http.Cookie{Name: "csrf", Value: "token-cookie", Path: "/"})
			io.WriteString(w, `<input name="__RequestVerificationToken" value="token">`)
			return
		}
		if r.FormValue("Login") != "alice" || r.FormValue("Password") != "secret" {
			io.WriteString(w, `<input name="Login"><input name="Password">`)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "active", Path: "/"})
		io.WriteString(w, `<html>authenticated</html>`)
	}))
}

func TestLoginCredentialSavingIsExplicitAndFailureIsWarning(t *testing.T) {
	server := loginServer(t)
	defer server.Close()
	configureAuthTest(t, server.URL)

	store := &fakeCredentialStore{}
	code, stdout, stderr := runWithStore(t, store, []string{"login", "--user", "alice", "--pass", "secret", "--json"}, "")
	if code != 0 || stderr != "" || store.setCalls != 0 || !strings.Contains(stdout, `"credentialsSaved":false`) {
		t.Fatalf("ordinary login = code %d stdout %q stderr %q sets %d", code, stdout, stderr, store.setCalls)
	}

	store.setErr = errors.New("vault unavailable")
	code, stdout, stderr = runWithStore(t, store, []string{"login", "--user", "alice", "--pass", "secret", "--save-credentials", "--json"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, `"credential_store_unavailable"`) || !strings.Contains(stdout, `"credentialsSaved":false`) {
		t.Fatalf("warning login = code %d stdout %q stderr %q", code, stdout, stderr)
	}

	store.setErr = nil
	code, stdout, stderr = runWithStore(t, store, []string{"login", "--user", "alice", "--pass", "secret", "--save-credentials", "--json"}, "")
	if code != 0 || stderr != "" || store.record.Username != "alice" || store.record.Password != "secret" || !strings.Contains(stdout, `"credentialsSaved":true`) {
		t.Fatalf("saved login = code %d stdout %q stderr %q record %#v", code, stdout, stderr, store.record)
	}

	previousSets := store.setCalls
	code, _, _ = runWithStore(t, store, []string{"login", "--user", "alice", "--pass", "wrong", "--save-credentials", "--json"}, "")
	if code != 3 || store.setCalls != previousSets {
		t.Fatalf("failed login = code %d, credential sets %d; want %d", code, store.setCalls, previousSets)
	}
}

func TestFileCredentialStoreSavesAndRenewsOnHeadlessSystems(t *testing.T) {
	readCalls, loginCalls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			if r.Method == http.MethodGet {
				io.WriteString(w, `<input name="__RequestVerificationToken" value="token">`)
				return
			}
			loginCalls++
			if r.FormValue("Login") != "alice" || r.FormValue("Password") != "secret" {
				io.WriteString(w, `<input name="Login"><input name="Password">`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "active", Path: "/"})
			io.WriteString(w, `<html>authenticated</html>`)
		case "/Worksheet/Read":
			readCalls++
			if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "active" {
				io.WriteString(w, `<input name="Login"><input name="Password">`)
				return
			}
			io.WriteString(w, `<table id="tbWorksheet"><tbody></tbody></table>`)
		}
	}))
	defer server.Close()
	configureAuthTest(t, server.URL)
	store := &fakeCredentialStore{getErr: &credentials.Error{Kind: credentials.KindUnavailable, Message: "vault unavailable"}}

	code, stdout, stderr := runWithStore(t, store, []string{"login", "--user", "alice", "--pass", "secret", "--save-credentials", "--credential-store", "file", "--json"}, "")
	if code != 0 || stderr != "" || store.setCalls != 0 || !strings.Contains(stdout, `"credentialsSaved":true`) || !strings.Contains(stdout, `"credentialStore":"file"`) {
		t.Fatalf("file login = code %d stdout %q stderr %q system sets %d", code, stdout, stderr, store.setCalls)
	}
	credentialsPath := os.Getenv("TIMESHEET_CREDENTIALS")
	info, err := os.Stat(credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials file mode = %v; want 0600", info.Mode().Perm())
	}
	if err := os.Remove(os.Getenv("TIMESHEET_SESSION")); err != nil {
		t.Fatal(err)
	}

	code, _, stderr = runWithStore(t, store, []string{"list", "--json"}, "")
	if code != 0 || stderr != "" || loginCalls != 2 || readCalls != 2 {
		t.Fatalf("file renewal = code %d stderr %q logins %d reads %d", code, stderr, loginCalls, readCalls)
	}
}

func TestFileCredentialStoreMustBeExplicit(t *testing.T) {
	configureAuthTest(t, "https://example.test")
	store := &fakeCredentialStore{}
	code, _, stderr := runWithStore(t, store, []string{"login", "--credential-store", "file", "--json"}, "")
	if code != 2 || !strings.Contains(stderr, `"code":"invalid_input"`) || store.setCalls != 0 {
		t.Fatalf("implicit file store = code %d stderr %q sets %d", code, stderr, store.setCalls)
	}
}

func TestLogoutLifecycleAndCredentialDeleteFailure(t *testing.T) {
	store := &fakeCredentialStore{}
	session := configureAuthTest(t, "https://example.test")
	if err := os.WriteFile(session, []byte(`{"session":"active"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runWithStore(t, store, []string{"logout", "--json"}, "")
	if code != 0 || stderr != "" || store.deleteCalls != 0 || !strings.Contains(stdout, `"sessionRemoved":true`) {
		t.Fatalf("logout = code %d stdout %q stderr %q", code, stdout, stderr)
	}
	code, stdout, stderr = runWithStore(t, store, []string{"logout", "--forget-credentials", "--json"}, "")
	if code != 0 || stderr != "" || store.deleteCalls != 1 || !strings.Contains(stdout, `"credentialsForgotten":true`) {
		t.Fatalf("forget = code %d stdout %q stderr %q", code, stdout, stderr)
	}
	store.deleteErr = errors.New("locked")
	code, stdout, stderr = runWithStore(t, store, []string{"logout", "--forget-credentials", "--json"}, "")
	if code != 1 || stdout != "" || !strings.Contains(stderr, `"code":"credential_store_error"`) {
		t.Fatalf("failed forget = code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestExpiredReadRenewsFromVaultOnce(t *testing.T) {
	readCalls, loginCalls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			if r.Method == http.MethodGet {
				io.WriteString(w, `<input name="__RequestVerificationToken" value="token">`)
				return
			}
			loginCalls++
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "active", Path: "/"})
			io.WriteString(w, `<html>authenticated</html>`)
		case "/Worksheet/Read":
			readCalls++
			if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "active" {
				io.WriteString(w, `<input name="Login"><input name="Password">`)
				return
			}
			io.WriteString(w, `<table id="tbWorksheet"><tbody></tbody></table>`)
		}
	}))
	defer server.Close()
	configureAuthTest(t, server.URL)
	store := &fakeCredentialStore{record: credentials.Record{Version: 1, Username: "alice", Password: "secret"}}
	code, _, stderr := runWithStore(t, store, []string{"list", "--json"}, "")
	if code != 0 || stderr != "" || store.getCalls != 1 || loginCalls != 1 || readCalls != 2 {
		t.Fatalf("renew = code %d stderr %q gets %d logins %d reads %d", code, stderr, store.getCalls, loginCalls, readCalls)
	}
}

func TestEnvironmentRenewalTakesPrecedenceAndRejectedVaultRecordIsDeleted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			if r.Method == http.MethodGet {
				io.WriteString(w, `<input name="__RequestVerificationToken" value="token">`)
				return
			}
			if r.FormValue("Login") != "alice" || r.FormValue("Password") != "secret" {
				io.WriteString(w, `<input name="Login"><input name="Password">`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "active", Path: "/"})
			io.WriteString(w, `<html>authenticated</html>`)
		case "/Worksheet/Read":
			if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "active" {
				io.WriteString(w, `<input name="Login"><input name="Password">`)
				return
			}
			io.WriteString(w, `<table id="tbWorksheet"><tbody></tbody></table>`)
		}
	}))
	defer server.Close()
	configureAuthTest(t, server.URL)
	t.Setenv("TIMESHEET_USER", "alice")
	t.Setenv("TIMESHEET_PASS", "secret")
	store := &fakeCredentialStore{getErr: errors.New("must not be called")}
	code, _, stderr := runWithStore(t, store, []string{"list", "--json"}, "")
	if code != 0 || stderr != "" || store.getCalls != 0 {
		t.Fatalf("environment renewal = code %d stderr %q vault gets %d", code, stderr, store.getCalls)
	}

	configureAuthTest(t, server.URL)
	t.Setenv("TIMESHEET_USER", "incomplete")
	store = &fakeCredentialStore{record: credentials.Record{Version: 1, Username: "alice", Password: "secret"}}
	code, _, stderr = runWithStore(t, store, []string{"list", "--json"}, "")
	if code != 0 || stderr != "" || store.getCalls != 1 {
		t.Fatalf("incomplete environment fallback = code %d stderr %q vault gets %d", code, stderr, store.getCalls)
	}

	configureAuthTest(t, server.URL)
	store = &fakeCredentialStore{getErr: &credentials.Error{Kind: credentials.KindUnavailable, Message: "vault unavailable"}}
	code, _, stderr = runWithStore(t, store, []string{"list", "--json"}, "")
	if code != 3 || !strings.Contains(stderr, `"code":"auth_required"`) {
		t.Fatalf("unavailable vault = code %d stderr %q", code, stderr)
	}

	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Worksheet/Read" {
			io.WriteString(w, `<input name="Login"><input name="Password">`)
			return
		}
		if r.Method == http.MethodGet {
			io.WriteString(w, `<input name="__RequestVerificationToken" value="token">`)
			return
		}
		io.WriteString(w, `<input name="Login"><input name="Password">`)
	}))
	defer rejecting.Close()
	configureAuthTest(t, rejecting.URL)
	store = &fakeCredentialStore{record: credentials.Record{Version: 1, Username: "alice", Password: "wrong"}}
	code, _, stderr = runWithStore(t, store, []string{"list", "--json"}, "")
	if code != 3 || store.deleteCalls != 1 || !strings.Contains(stderr, `"code":"login_failed"`) {
		t.Fatalf("rejected = code %d deletes %d stderr %q", code, store.deleteCalls, stderr)
	}
}

func TestMutationRenewsButIsNotRetried(t *testing.T) {
	deleteCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Worksheet/Delete":
			deleteCalls++
			io.WriteString(w, `<input name="Login"><input name="Password">`)
		case "/":
			if r.Method == http.MethodGet {
				io.WriteString(w, `<input name="__RequestVerificationToken" value="token">`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "active", Path: "/"})
			io.WriteString(w, `<html>authenticated</html>`)
		}
	}))
	defer server.Close()
	configureAuthTest(t, server.URL)
	store := &fakeCredentialStore{record: credentials.Record{Version: 1, Username: "alice", Password: "secret"}}
	code, _, stderr := runWithStore(t, store, []string{"delete", "123", "--yes", "--json"}, "")
	if code != 3 || deleteCalls != 1 || !strings.Contains(stderr, "session renewed") {
		t.Fatalf("mutation = code %d deletes %d stderr %q", code, deleteCalls, stderr)
	}
}
