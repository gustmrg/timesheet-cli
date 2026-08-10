package updater_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gustmrg/timesheet-cli/internal/updater"
)

func TestUpdaterDownloadsVerifiedLatestReleaseAndReplacesExecutable(t *testing.T) {
	const version = "0.6.0"
	binaryName := "timesheet"
	archiveExtension := "tar.gz"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
		archiveExtension = "zip"
	}
	archiveName := fmt.Sprintf("timesheet-cli_%s_%s_%s.%s", version, runtime.GOOS, runtime.GOARCH, archiveExtension)
	archive := releaseArchive(t, binaryName, []byte("new executable"), archiveExtension)
	digest := sha256.Sum256(archive)
	checksums := []byte(fmt.Sprintf("%x  %s\n", digest, archiveName))

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("authorization header = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/releases/latest":
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Errorf("metadata Accept = %q", r.Header.Get("Accept"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v" + version,
				"assets": []map[string]any{
					{"name": archiveName, "url": server.URL + "/assets/archive"},
					{"name": "checksums.txt", "url": server.URL + "/assets/checksums"},
				},
			})
		case "/assets/archive":
			if r.Header.Get("Accept") != "application/octet-stream" {
				t.Errorf("archive Accept = %q", r.Header.Get("Accept"))
			}
			_, _ = w.Write(archive)
		case "/assets/checksums":
			_, _ = w.Write(checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(target, []byte("old executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	client, err := updater.New(updater.Config{
		APIURL:         server.URL + "/releases/latest",
		CurrentVersion: "0.5.0",
		Executable:     target,
		Token:          "test-token",
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated || result.CurrentVersion != "0.5.0" || result.LatestVersion != version || result.Executable != resolvedTarget {
		t.Fatalf("unexpected result: %#v", result)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new executable" {
		t.Fatalf("executable contents = %q", got)
	}
}

func TestUpdaterDoesNotDownloadAssetsWhenAlreadyCurrent(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/releases/latest" {
			t.Errorf("unexpected asset request: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.5.0", "assets": []any{}})
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "timesheet")
	if err := os.WriteFile(target, []byte("current executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	client, err := updater.New(updater.Config{
		APIURL: server.URL + "/releases/latest", CurrentVersion: "v0.5.0",
		Executable: target, OS: runtime.GOOS, Arch: runtime.GOARCH, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated || result.CurrentVersion != "0.5.0" || result.LatestVersion != "0.5.0" || requests != 1 {
		t.Fatalf("result=%#v requests=%d", result, requests)
	}
}

func TestUpdaterRejectsChecksumMismatchWithoutReplacingExecutable(t *testing.T) {
	const version = "0.6.0"
	binaryName := "timesheet"
	archiveExtension := "tar.gz"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
		archiveExtension = "zip"
	}
	archiveName := fmt.Sprintf("timesheet-cli_%s_%s_%s.%s", version, runtime.GOOS, runtime.GOARCH, archiveExtension)
	archive := releaseArchive(t, binaryName, []byte("untrusted executable"), archiveExtension)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v" + version,
				"assets": []map[string]any{
					{"name": archiveName, "url": server.URL + "/assets/archive"},
					{"name": "checksums.txt", "url": server.URL + "/assets/checksums"},
				},
			})
		case "/assets/archive":
			_, _ = w.Write(archive)
		case "/assets/checksums":
			_, _ = fmt.Fprintf(w, "%064d  %s\n", 0, archiveName)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	target := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(target, []byte("trusted executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	client, err := updater.New(updater.Config{
		APIURL: server.URL + "/releases/latest", CurrentVersion: "0.5.0",
		Executable: target, OS: runtime.GOOS, Arch: runtime.GOARCH, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Update(context.Background()); err == nil || !strings.Contains(err.Error(), "checksum verification failed") {
		t.Fatalf("Update() error = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "trusted executable" {
		t.Fatalf("executable was replaced after checksum failure: %q", got)
	}
}

func releaseArchive(t *testing.T, binaryName string, contents []byte, extension string) []byte {
	t.Helper()
	var output bytes.Buffer
	switch extension {
	case "tar.gz":
		compressed := gzip.NewWriter(&output)
		archive := tar.NewWriter(compressed)
		if err := archive.WriteHeader(&tar.Header{Name: binaryName, Mode: 0o755, Size: int64(len(contents)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(contents); err != nil {
			t.Fatal(err)
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := compressed.Close(); err != nil {
			t.Fatal(err)
		}
	case "zip":
		archive := zip.NewWriter(&output)
		file, err := archive.Create(binaryName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(file, bytes.NewReader(contents)); err != nil {
			t.Fatal(err)
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported test archive extension: %s", extension)
	}
	return output.Bytes()
}
