package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gustmrg/timesheet-cli/internal/cli"
)

func TestUpgradeCommandReportsWhenLatestReleaseIsAlreadyInstalled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/latest" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer preferred-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.5.0", "assets": []any{}})
	}))
	defer server.Close()
	t.Setenv("TIMESHEET_UPGRADE_API_URL", server.URL+"/releases/latest")
	t.Setenv("GH_TOKEN", "preferred-token")
	t.Setenv("GITHUB_TOKEN", "fallback-token")

	var stdout, stderr bytes.Buffer
	code := cli.Execute([]string{"upgrade", "--json"}, strings.NewReader(""), &stdout, &stderr, "0.5.0")
	if code != 0 || stderr.String() != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			CurrentVersion string `json:"currentVersion"`
			LatestVersion  string `json:"latestVersion"`
			Updated        bool   `json:"updated"`
			Executable     string `json:"executable"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.CurrentVersion != "0.5.0" || envelope.Data.LatestVersion != "0.5.0" || envelope.Data.Updated || envelope.Data.Executable == "" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestUpdateCommandStillRequiresAnEntryID(t *testing.T) {
	configure(t, "https://example.invalid")
	code, stdout, stderr := execute(t, []string{"update", "--json"}, "")
	if code != 2 || stdout != "" || !strings.Contains(stderr, `"code":"usage"`) {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
