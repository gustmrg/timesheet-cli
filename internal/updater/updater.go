package updater

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	maxArchiveSize  = 100 << 20
	maxChecksumSize = 1 << 20
)

type Config struct {
	APIURL         string
	CurrentVersion string
	Executable     string
	Token          string
	OS             string
	Arch           string
	HTTPClient     *http.Client
}

type Result struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Updated        bool   `json:"updated"`
	Executable     string `json:"executable"`
}

type Updater struct {
	apiURL         string
	currentVersion string
	executable     string
	token          string
	os             string
	arch           string
	http           *http.Client
}

type releaseMetadata struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func New(config Config) (*Updater, error) {
	parsed, err := url.Parse(config.APIURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid update API URL")
	}
	if config.Executable == "" {
		return nil, fmt.Errorf("could not determine the current executable")
	}
	executable, err := filepath.Abs(config.Executable)
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	targetOS := config.OS
	if targetOS == "" {
		targetOS = runtime.GOOS
	}
	targetArch := config.Arch
	if targetArch == "" {
		targetArch = runtime.GOARCH
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &Updater{
		apiURL: config.APIURL, currentVersion: normalizedCurrentVersion(config.CurrentVersion),
		executable: executable, token: config.Token, os: targetOS, arch: targetArch, http: client,
	}, nil
}

func (u *Updater) Update(ctx context.Context) (Result, error) {
	result := Result{CurrentVersion: u.currentVersion, Executable: u.executable}
	metadataBody, err := u.download(ctx, u.apiURL, "application/vnd.github+json", maxChecksumSize)
	if err != nil {
		return result, fmt.Errorf("retrieve latest release: %w", err)
	}
	var release releaseMetadata
	if err := json.Unmarshal(metadataBody, &release); err != nil {
		return result, fmt.Errorf("decode latest release metadata: %w", err)
	}
	latestVersion, err := normalizeReleaseVersion(release.TagName)
	if err != nil {
		return result, err
	}
	result.LatestVersion = latestVersion
	if !isOlder(u.currentVersion, latestVersion) {
		return result, nil
	}

	archiveName, binaryName, archiveFormat, err := releaseNames(latestVersion, u.os, u.arch)
	if err != nil {
		return result, err
	}
	archiveAsset, ok := findAsset(release.Assets, archiveName)
	if !ok {
		return result, fmt.Errorf("release asset was not found: %s", archiveName)
	}
	checksumAsset, ok := findAsset(release.Assets, "checksums.txt")
	if !ok {
		return result, fmt.Errorf("release asset was not found: checksums.txt")
	}
	archive, err := u.download(ctx, archiveAsset.URL, "application/octet-stream", maxArchiveSize)
	if err != nil {
		return result, fmt.Errorf("download %s: %w", archiveName, err)
	}
	checksums, err := u.download(ctx, checksumAsset.URL, "application/octet-stream", maxChecksumSize)
	if err != nil {
		return result, fmt.Errorf("download checksums.txt: %w", err)
	}
	expected, err := checksumFor(checksums, archiveName)
	if err != nil {
		return result, err
	}
	actual := sha256.Sum256(archive)
	if !strings.EqualFold(expected, hex.EncodeToString(actual[:])) {
		return result, fmt.Errorf("checksum verification failed for %s", archiveName)
	}
	binary, err := extractBinary(archive, archiveFormat, binaryName)
	if err != nil {
		return result, err
	}
	if err := installBinary(u.executable, binary); err != nil {
		return result, fmt.Errorf("replace executable: %w", err)
	}
	result.Updated = true
	return result, nil
}

func (u *Updater) download(ctx context.Context, target, accept string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "timesheet-cli/"+u.currentVersion)
	if u.token != "" {
		request.Header.Set("Authorization", "Bearer "+u.token)
	}
	response, err := u.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeded the %d byte limit", limit)
	}
	return body, nil
}

func findAsset(assets []releaseAsset, name string) (releaseAsset, bool) {
	for _, asset := range assets {
		if asset.Name == name && asset.URL != "" {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func checksumFor(checksums []byte, archiveName string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(checksums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && strings.TrimPrefix(fields[1], "*") == archiveName {
			if len(fields[0]) != sha256.Size*2 {
				return "", fmt.Errorf("invalid checksum for %s", archiveName)
			}
			if _, err := hex.DecodeString(fields[0]); err != nil {
				return "", fmt.Errorf("invalid checksum for %s", archiveName)
			}
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	return "", fmt.Errorf("archive checksum was not found: %s", archiveName)
}

func releaseNames(version, targetOS, targetArch string) (archiveName, binaryName, archiveFormat string, err error) {
	if targetArch != "amd64" && targetArch != "arm64" {
		return "", "", "", fmt.Errorf("unsupported architecture: %s", targetArch)
	}
	switch targetOS {
	case "darwin", "linux":
		binaryName = "timesheet"
		archiveFormat = "tar.gz"
	case "windows":
		binaryName = "timesheet.exe"
		archiveFormat = "zip"
	default:
		return "", "", "", fmt.Errorf("unsupported operating system: %s", targetOS)
	}
	archiveName = fmt.Sprintf("timesheet-cli_%s_%s_%s.%s", version, targetOS, targetArch, archiveFormat)
	return archiveName, binaryName, archiveFormat, nil
}

func normalizedCurrentVersion(version string) string {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if version == "" {
		return "dev"
	}
	return version
}

func normalizeReleaseVersion(version string) (string, error) {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if _, ok := numericVersion(version); !ok {
		return "", fmt.Errorf("latest release has an invalid version: %q", version)
	}
	return version, nil
}

func isOlder(current, latest string) bool {
	currentParts, currentOK := numericVersion(current)
	latestParts, latestOK := numericVersion(latest)
	if !latestOK {
		return false
	}
	if !currentOK {
		return current != latest
	}
	for index := range currentParts {
		if currentParts[index] != latestParts[index] {
			return currentParts[index] < latestParts[index]
		}
	}
	return false
}

func numericVersion(version string) ([3]int, bool) {
	var result [3]int
	if strings.ContainsAny(version, "+-") {
		return result, false
	}
	parts := strings.Split(version, ".")
	if len(parts) != len(result) {
		return result, false
	}
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || (len(part) > 1 && part[0] == '0') {
			return [3]int{}, false
		}
		result[index] = value
	}
	return result, true
}

func installBinary(executable string, binary []byte) error {
	info, err := os.Stat(executable)
	if err != nil {
		return fmt.Errorf("inspect current executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("current executable is not a regular file: %s", executable)
	}
	temporary, err := os.CreateTemp(filepath.Dir(executable), ".timesheet-update-*")
	if err != nil {
		return fmt.Errorf("create replacement beside %s: %w", executable, err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	mode := info.Mode().Perm() | 0o111
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return fmt.Errorf("set replacement permissions: %w", err)
	}
	if _, err := temporary.Write(binary); err != nil {
		temporary.Close()
		return fmt.Errorf("write replacement: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync replacement: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close replacement: %w", err)
	}
	if err := replaceExecutable(temporaryName, executable); err != nil {
		return err
	}
	return nil
}
