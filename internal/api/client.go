package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/gustmrg/timesheet-cli/internal/webparse"
)

type Kind string

const (
	KindAuth            Kind = "auth"
	KindLoginFailed     Kind = "login_failed"
	KindNetwork         Kind = "network"
	KindInvalidResponse Kind = "invalid_response"
	KindOperation       Kind = "operation"
)

type Error struct {
	Kind    Kind
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func IsKind(err error, kind Kind) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Kind == kind
}

type Config struct {
	BaseURL      string
	SessionFile  string
	RenewSession RenewSessionFunc
}

type RenewSessionFunc func(client *Client) error

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Client struct {
	baseURL          *url.URL
	sessionFile      string
	jar              http.CookieJar
	http             *http.Client
	renewSession     RenewSessionFunc
	renewAttempted   bool
	persistedCookies map[string]string
	warnings         []Warning
}

type Ref struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type EntryInput struct {
	Customer        Ref
	Project         Ref
	Category        Ref
	Date            string
	Start           string
	End             string
	Description     string
	DescriptionHTML bool
	NotMonetize     bool
}

type DropDownQuery struct {
	CustomerID int64
	ProjectID  int64
}

type DropDownRecord struct {
	IDCustomer   int64  `json:"IdCustomer"`
	CustomerName string `json:"CustomerName"`
	IDProject    int64  `json:"IdProject"`
	ProjectName  string `json:"ProjectName"`
	IDCategory   int64  `json:"IdCategory"`
	CategoryName string `json:"CategoryName"`
	IDDeveloper  int64  `json:"IdDeveloper"`
	IsDeleted    bool   `json:"IsDeleted"`
}

type WorksheetRecord struct {
	ID           int64  `json:"Id"`
	IDCustomer   int64  `json:"IdCustomer"`
	IDProject    int64  `json:"IdProject"`
	IDCategory   int64  `json:"IdCategory"`
	InformedDate string `json:"InformedDate"`
	StartTime    string `json:"StartTime"`
	EndTime      string `json:"EndTime"`
	Description  string `json:"Description"`
	NotMonetize  bool   `json:"NotMonetize"`
}

type EvaluateInfo struct {
	ManagerName   *string `json:"ManagerName"`
	Created       *string `json:"Created"`
	IsWait        string  `json:"IsWait"`
	IsApprove     string  `json:"IsApprove"`
	IsReprove     string  `json:"IsReprove"`
	IsPreApproved string  `json:"IsPreApproved"`
	IsPreReproved string  `json:"IsPreReproved"`
	IsReview      string  `json:"IsReview"`
}

type Created struct{ ID int64 }

type writeResponse struct {
	Success           bool   `json:"success"`
	Message           string `json:"message"`
	CreatedWorksheets []struct {
		ID int64 `json:"Id"`
	} `json:"createdWorksheets"`
}

func New(config Config) (*Client, error) {
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, &Error{Kind: KindInvalidResponse, Message: "invalid timesheet base URL", Cause: err}
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, &Error{Kind: KindInvalidResponse, Message: "create cookie jar", Cause: err}
	}
	client := &Client{
		baseURL:          baseURL,
		sessionFile:      config.SessionFile,
		jar:              jar,
		renewSession:     config.RenewSession,
		persistedCookies: make(map[string]string),
	}
	client.http = &http.Client{Jar: jar}
	if err := client.loadSession(); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) Login(user, password string) error {
	body, err := c.text(http.MethodGet, "/", nil, nil, "", false)
	if err != nil {
		return err
	}
	token := webparse.VerificationToken(body)
	if token == "" {
		return &Error{Kind: KindInvalidResponse, Message: "could not find __RequestVerificationToken on login page"}
	}
	form := url.Values{"__RequestVerificationToken": {token}, "Login": {user}, "Password": {password}}
	body, err = c.text(http.MethodPost, "/", nil, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded; charset=UTF-8", false)
	if err != nil {
		return err
	}
	if webparse.IsLoginPage(body) {
		return &Error{Kind: KindLoginFailed, Message: "login failed — check your credentials"}
	}
	return c.saveSession()
}

func (c *Client) ListEntries() ([]webparse.Entry, error) {
	return withReadRetry(c, c.listEntries)
}

func (c *Client) listEntries() ([]webparse.Entry, error) {
	body, err := c.readPage()
	if err != nil {
		return nil, err
	}
	entries, err := webparse.Entries(bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Kind: KindInvalidResponse, Message: err.Error(), Cause: err}
	}
	return entries, nil
}

func (c *Client) Metadata() (map[string][]webparse.Option, error) {
	return withReadRetry(c, c.metadata)
}

func (c *Client) metadata() (map[string][]webparse.Option, error) {
	body, err := c.readPage()
	if err != nil {
		return nil, err
	}
	metadata, err := webparse.Selects(bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Kind: KindInvalidResponse, Message: err.Error(), Cause: err}
	}
	return metadata, nil
}

func (c *Client) DropDownChange(query DropDownQuery) ([]DropDownRecord, error) {
	return withReadRetry(c, func() ([]DropDownRecord, error) {
		return c.dropDownChange(query)
	})
}

func (c *Client) dropDownChange(query DropDownQuery) ([]DropDownRecord, error) {
	values := url.Values{}
	if query.CustomerID != 0 {
		values.Set("idcustomer", strconv.FormatInt(query.CustomerID, 10))
	}
	if query.ProjectID != 0 {
		values.Set("idproject", strconv.FormatInt(query.ProjectID, 10))
	}
	var result []DropDownRecord
	if err := c.readJSON(http.MethodGet, "/Worksheet/DropDownChange", values, nil, "", true, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Worksheet(id int64) (WorksheetRecord, error) {
	return withReadRetry(c, func() (WorksheetRecord, error) {
		return c.worksheet(id)
	})
}

func (c *Client) worksheet(id int64) (WorksheetRecord, error) {
	var result WorksheetRecord
	query := url.Values{"id": {strconv.FormatInt(id, 10)}}
	err := c.readJSON(http.MethodGet, "/Worksheet/Update", query, nil, "", true, &result)
	return result, err
}

func (c *Client) Evaluate(worksheetID, evaluateID int64) (EvaluateInfo, error) {
	return withReadRetry(c, func() (EvaluateInfo, error) {
		return c.evaluate(worksheetID, evaluateID)
	})
}

func (c *Client) evaluate(worksheetID, evaluateID int64) (EvaluateInfo, error) {
	var result EvaluateInfo
	form := url.Values{"idworksheet": {strconv.FormatInt(worksheetID, 10)}, "idevaluate": {strconv.FormatInt(evaluateID, 10)}}
	err := c.readJSON(http.MethodPost, "/Worksheet/ReadEvaluate", nil, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded; charset=UTF-8", true, &result)
	return result, err
}

func (c *Client) Create(entry EntryInput) (Created, error) {
	description := entry.Description
	if !entry.DescriptionHTML {
		description = plainTextHTML(description)
	}
	item := map[string]any{
		"IdCustomer": strconv.FormatInt(entry.Customer.ID, 10), "CustomerName": entry.Customer.Name,
		"IdProject": strconv.FormatInt(entry.Project.ID, 10), "ProjectName": entry.Project.Name,
		"IdCategory": strconv.FormatInt(entry.Category.ID, 10), "CategoryName": entry.Category.Name,
		"InformedDate": entry.Date, "StartTime": entry.Start, "EndTime": entry.End,
		"Description": description, "NotMonetize": entry.NotMonetize,
	}
	var result writeResponse
	if err := c.sendJSON("/Worksheet/UpdateMultiple", map[string]any{"WorksheetMultiple": []any{item}}, &result); err != nil {
		return Created{}, err
	}
	if !result.Success {
		return Created{}, operationError(result.Message, "create failed")
	}
	if len(result.CreatedWorksheets) == 0 {
		return Created{}, &Error{Kind: KindInvalidResponse, Message: "create response did not contain an entry ID"}
	}
	return Created{ID: result.CreatedWorksheets[0].ID}, nil
}

func (c *Client) Update(id int64, entry EntryInput) error {
	description := entry.Description
	if !entry.DescriptionHTML {
		description = plainTextHTML(description)
	}
	payload := map[string]any{
		"id":         strconv.FormatInt(id, 10),
		"idcustomer": strconv.FormatInt(entry.Customer.ID, 10), "customername": entry.Customer.Name,
		"idproject": strconv.FormatInt(entry.Project.ID, 10), "projectname": entry.Project.Name,
		"idcategory": strconv.FormatInt(entry.Category.ID, 10), "categoryname": entry.Category.Name,
		"informeddate": entry.Date, "starttime": entry.Start, "endtime": entry.End,
		"description": description, "notmonetize": entry.NotMonetize,
	}
	var result writeResponse
	if err := c.sendJSON("/Worksheet/UpdateOne", payload, &result); err != nil {
		return err
	}
	if !result.Success {
		return operationError(result.Message, "update failed")
	}
	return nil
}

func (c *Client) Delete(id int64) error {
	form := url.Values{"id": {strconv.FormatInt(id, 10)}}
	var result writeResponse
	if err := c.readJSON(http.MethodPost, "/Worksheet/Delete", nil, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded; charset=UTF-8", true, &result); err != nil {
		return err
	}
	if !result.Success {
		return operationError(result.Message, "delete failed")
	}
	return nil
}

func (c *Client) sendJSON(path string, payload any, target any) error {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return &Error{Kind: KindInvalidResponse, Message: "encode request", Cause: err}
	}
	return c.readJSON(http.MethodPost, path, nil, &body, "application/json", true, target)
}

func (c *Client) readPage() ([]byte, error) {
	body, err := c.text(http.MethodGet, "/Worksheet/Read", nil, nil, "", false)
	if err != nil {
		return nil, err
	}
	if webparse.IsLoginPage(body) {
		return nil, &Error{Kind: KindAuth, Message: "session expired or missing; run `timesheet login` first"}
	}
	c.persistSessionBestEffort()
	return body, nil
}

func (c *Client) readJSON(method, path string, query url.Values, body io.Reader, contentType string, xhr bool, target any) error {
	response, err := c.text(method, path, query, body, contentType, xhr)
	if err != nil {
		return err
	}
	if webparse.IsLoginPage(response) {
		return &Error{Kind: KindAuth, Message: "session expired or missing; run `timesheet login` first"}
	}
	if err := json.Unmarshal(response, target); err != nil {
		preview := string(response)
		if len(preview) > 120 {
			preview = preview[:120]
		}
		return &Error{Kind: KindInvalidResponse, Message: fmt.Sprintf("expected JSON from %s, got: %s", path, preview), Cause: err}
	}
	c.persistSessionBestEffort()
	return nil
}

func (c *Client) text(method, path string, query url.Values, body io.Reader, contentType string, xhr bool) ([]byte, error) {
	reference := &url.URL{Path: path}
	target := c.baseURL.ResolveReference(reference)
	if query != nil {
		target.RawQuery = query.Encode()
	}
	request, err := http.NewRequest(method, target.String(), body)
	if err != nil {
		return nil, &Error{Kind: KindInvalidResponse, Message: "build HTTP request", Cause: err}
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if xhr {
		request.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, &Error{Kind: KindNetwork, Message: "timesheet request failed", Cause: err}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, &Error{Kind: KindNetwork, Message: "read timesheet response", Cause: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &Error{Kind: KindInvalidResponse, Message: fmt.Sprintf("timesheet returned HTTP %d", response.StatusCode)}
	}
	return responseBody, nil
}

func (c *Client) loadSession() error {
	body, err := os.ReadFile(c.sessionFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return &Error{Kind: KindInvalidResponse, Message: "read session file", Cause: err}
	}
	var stored map[string]string
	if err := json.Unmarshal(body, &stored); err != nil {
		return &Error{Kind: KindInvalidResponse, Message: "session file contains invalid JSON", Cause: err}
	}
	cookies := make([]*http.Cookie, 0, len(stored))
	for name, value := range stored {
		cookies = append(cookies, &http.Cookie{Name: name, Value: value, Path: "/"})
	}
	c.jar.SetCookies(c.baseURL, cookies)
	c.persistedCookies = cloneCookies(stored)
	return nil
}

func (c *Client) saveSession() error {
	stored := c.cookieSnapshot()
	if err := c.writeSession(stored); err != nil {
		return err
	}
	c.persistedCookies = cloneCookies(stored)
	return nil
}

func (c *Client) writeSession(stored map[string]string) error {
	directory := filepath.Dir(c.sessionFile)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return &Error{Kind: KindInvalidResponse, Message: "create session directory", Cause: err}
	}
	_ = os.Chmod(directory, 0o700)
	temporary, err := os.CreateTemp(directory, ".session-*.tmp")
	if err != nil {
		return &Error{Kind: KindInvalidResponse, Message: "create temporary session file", Cause: err}
	}
	name := temporary.Name()
	defer os.Remove(name)
	_ = temporary.Chmod(0o600)
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(stored)
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		if runtime.GOOS == "windows" {
			_ = os.Remove(c.sessionFile)
		}
		err = os.Rename(name, c.sessionFile)
	}
	if err != nil {
		return &Error{Kind: KindInvalidResponse, Message: "save session file", Cause: err}
	}
	return nil
}

func (c *Client) persistSessionBestEffort() {
	stored := c.cookieSnapshot()
	if maps.Equal(stored, c.persistedCookies) {
		return
	}
	if err := c.writeSession(stored); err != nil {
		c.addWarning(Warning{
			Code:    "session_persist_failed",
			Message: "authenticated, but could not persist the renewed session",
		})
		return
	}
	c.persistedCookies = cloneCookies(stored)
}

func (c *Client) cookieSnapshot() map[string]string {
	stored := make(map[string]string)
	for _, cookie := range c.jar.Cookies(c.baseURL) {
		stored[cookie.Name] = cookie.Value
	}
	return stored
}

func cloneCookies(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for name, value := range source {
		cloned[name] = value
	}
	return cloned
}

func (c *Client) addWarning(warning Warning) {
	for _, existing := range c.warnings {
		if existing.Code == warning.Code {
			return
		}
	}
	c.warnings = append(c.warnings, warning)
}

func (c *Client) Warnings() []Warning {
	return append([]Warning(nil), c.warnings...)
}

func (c *Client) Renew() error {
	if c.renewSession == nil {
		return &Error{Kind: KindAuth, Message: "session expired or missing; run `timesheet login` first"}
	}
	if c.renewAttempted {
		return &Error{Kind: KindAuth, Message: "session renewal was already attempted; run `timesheet login`"}
	}
	c.renewAttempted = true
	return c.renewSession(c)
}

func withReadRetry[T any](c *Client, operation func() (T, error)) (T, error) {
	result, err := operation()
	if err == nil || !IsKind(err, KindAuth) || c.renewSession == nil || c.renewAttempted {
		return result, err
	}
	if renewErr := c.Renew(); renewErr != nil {
		var zero T
		return zero, renewErr
	}
	return operation()
}

var paragraphBreak = regexp.MustCompile(`\n{2,}`)

func plainTextHTML(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	paragraphs := paragraphBreak.Split(value, -1)
	for index, paragraph := range paragraphs {
		paragraphs[index] = "<p>" + strings.ReplaceAll(html.EscapeString(paragraph), "\n", "<br>") + "</p>"
	}
	return strings.Join(paragraphs, "")
}

func operationError(message, fallback string) error {
	if message == "" {
		message = fallback
	}
	return &Error{Kind: KindOperation, Message: message}
}
