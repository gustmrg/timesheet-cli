package cli

import (
	"fmt"
	"strings"

	"github.com/gustmrg/timesheet-cli/internal/api"
	"github.com/gustmrg/timesheet-cli/internal/webparse"
)

// The operations below hold the behavior shared by the cobra commands and the
// MCP tools. They validate their own input, return the data placed in the JSON
// envelope, and never print.

type entryList struct {
	Entries  []webparse.Entry `json:"entries"`
	Returned int              `json:"returned"`
	Total    int              `json:"total"`
}

type entryStatus struct {
	EntryID int64   `json:"entryId"`
	State   string  `json:"state"`
	Manager *string `json:"manager"`
	Created *string `json:"created"`
}

type entryDetails struct {
	EntryID     int64  `json:"entryId"`
	Date        string `json:"date"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Total       string `json:"total"`
	Customer    string `json:"customer"`
	Project     string `json:"project"`
	Category    string `json:"category"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

type entryMutation struct {
	Action string          `json:"action"`
	Entry  normalizedEntry `json:"entry"`
}

type entryDeletion struct {
	ID      int64 `json:"id"`
	Deleted bool  `json:"deleted"`
}

type invoiceListOptions struct {
	limit, offset  int
	search, status string
	all            bool
}

func (a *app) listEntries(limit int, all bool) (entryList, error) {
	if limit <= 0 {
		return entryList{}, fail("invalid_input", "--limit must be a positive integer", 2, nil)
	}
	client, err := a.client()
	if err != nil {
		return entryList{}, err
	}
	entries, apiErr := client.ListEntries()
	if apiErr != nil {
		return entryList{}, classifyAPI(apiErr)
	}
	total := len(entries)
	if !all && len(entries) > limit {
		entries = entries[:limit]
	}
	return entryList{Entries: entries, Returned: len(entries), Total: total}, nil
}

func (a *app) metadata() ([]metaCustomer, error) {
	client, err := a.client()
	if err != nil {
		return nil, err
	}
	customers, apiErr := loadMeta(client)
	if apiErr != nil {
		return nil, classifyAPI(apiErr)
	}
	return customers, nil
}

// evaluatedEntry finds an entry in the list and reads its evaluation, which
// carries the approval state and the description.
func (a *app) evaluatedEntry(id int64) (webparse.Entry, api.EvaluateInfo, error) {
	client, err := a.client()
	if err != nil {
		return webparse.Entry{}, api.EvaluateInfo{}, err
	}
	entries, apiErr := client.ListEntries()
	if apiErr != nil {
		return webparse.Entry{}, api.EvaluateInfo{}, classifyAPI(apiErr)
	}
	var entry *webparse.Entry
	for index := range entries {
		if entries[index].ID == id {
			entry = &entries[index]
			break
		}
	}
	if entry == nil {
		return webparse.Entry{}, api.EvaluateInfo{}, fail("not_found", fmt.Sprintf("entry %d not found", id), 5, nil)
	}
	if entry.EvaluateID == nil {
		return webparse.Entry{}, api.EvaluateInfo{}, fail("not_found", fmt.Sprintf("entry %d has no evaluation", id), 5, nil)
	}
	evaluation, apiErr := client.Evaluate(id, *entry.EvaluateID)
	if apiErr != nil {
		return webparse.Entry{}, api.EvaluateInfo{}, classifyAPI(apiErr)
	}
	return *entry, evaluation, nil
}

func (a *app) entryStatus(id int64) (entryStatus, webparse.Entry, error) {
	entry, evaluation, err := a.evaluatedEntry(id)
	if err != nil {
		return entryStatus{}, webparse.Entry{}, err
	}
	status := entryStatus{EntryID: id, State: evaluationState(evaluation), Manager: evaluation.ManagerName, Created: evaluation.Created}
	return status, entry, nil
}

func (a *app) entryDetails(id int64) (entryDetails, error) {
	entry, evaluation, err := a.evaluatedEntry(id)
	if err != nil {
		return entryDetails{}, err
	}
	description := evaluation.Description
	if text, textErr := webparse.FragmentText(description); textErr == nil {
		description = text
	}
	return entryDetails{
		EntryID: id, Date: entry.Date, Start: entry.Start, End: entry.End, Total: entry.Total,
		Customer: entry.Customer, Project: entry.Project, Category: entry.Category,
		Status: entry.Status, Description: description,
	}, nil
}

func (a *app) createEntry(flags entryFlags) (entryMutation, error) {
	if flags.monetize {
		return entryMutation{}, fail("invalid_input", "--monetize is only meaningful when updating an entry", 2, nil)
	}
	if err := validateEntrySyntax(flags, true); err != nil {
		return entryMutation{}, err
	}
	client, err := a.client()
	if err != nil {
		return entryMutation{}, err
	}
	entry, normalized, resolveErr := resolveEntry(client, flags, nil)
	if resolveErr != nil {
		return entryMutation{}, resolveErr
	}
	created, apiErr := client.Create(entry)
	if apiErr != nil {
		return entryMutation{}, a.classifyMutationError(client, apiErr)
	}
	normalized.ID = created.ID
	return entryMutation{Action: "created", Entry: normalized}, nil
}

// updateEntry changes only the fields set in flags: an empty string keeps the
// current value, and monetize/notMonetize switch the monetization flag.
func (a *app) updateEntry(id int64, flags entryFlags) (entryMutation, error) {
	if flags.monetize && flags.notMonetize {
		return entryMutation{}, fail("invalid_input", "--monetize and --not-monetize are mutually exclusive", 2, nil)
	}
	if err := validateEntrySyntax(flags, false); err != nil {
		return entryMutation{}, err
	}
	client, err := a.client()
	if err != nil {
		return entryMutation{}, err
	}
	current, apiErr := client.Worksheet(id)
	if apiErr != nil {
		return entryMutation{}, classifyAPI(apiErr)
	}
	if current.ID == 0 {
		return entryMutation{}, fail("not_found", fmt.Sprintf("entry %d not found", id), 5, nil)
	}
	base := &entryBase{customer: api.Ref{ID: current.IDCustomer}, project: api.Ref{ID: current.IDProject}, category: api.Ref{ID: current.IDCategory}, date: current.InformedDate, start: current.StartTime, end: current.EndTime, description: current.Description, descriptionHTML: flags.description == "", notMonetize: current.NotMonetize}
	if flags.notMonetize {
		base.notMonetize = true
	}
	if flags.monetize {
		base.notMonetize = false
	}
	entry, normalized, resolveErr := resolveEntry(client, flags, base)
	if resolveErr != nil {
		return entryMutation{}, resolveErr
	}
	if apiErr := client.Update(id, entry); apiErr != nil {
		return entryMutation{}, a.classifyMutationError(client, apiErr)
	}
	normalized.ID = id
	return entryMutation{Action: "updated", Entry: normalized}, nil
}

func (a *app) deleteEntry(id int64) (entryDeletion, error) {
	client, err := a.client()
	if err != nil {
		return entryDeletion{}, err
	}
	if apiErr := client.Delete(id); apiErr != nil {
		return entryDeletion{}, a.classifyMutationError(client, apiErr)
	}
	return entryDeletion{ID: id, Deleted: true}, nil
}

func (a *app) listInvoices(options invoiceListOptions) (api.InvoicePage, error) {
	if options.limit <= 0 {
		return api.InvoicePage{}, fail("invalid_input", "--limit must be a positive integer", 2, nil)
	}
	if options.offset < 0 {
		return api.InvoicePage{}, fail("invalid_input", "--offset cannot be negative", 2, nil)
	}
	status := strings.ToLower(strings.TrimSpace(options.status))
	if status != "" && status != "pending" && status != "sent" {
		return api.InvoicePage{}, fail("invalid_input", "--status must be `pending` or `sent`", 2, nil)
	}
	if options.all {
		options.offset = 0
		options.limit = allInvoiceRows
	}
	client, err := a.client()
	if err != nil {
		return api.InvoicePage{}, err
	}
	page, apiErr := client.ListInvoices(api.InvoiceQuery{Offset: options.offset, Limit: options.limit, Search: options.search, Status: status})
	if apiErr != nil {
		return api.InvoicePage{}, classifyAPI(apiErr)
	}
	return page, nil
}

func (a *app) invoicePreview(processID int64) (api.InvoicePreview, error) {
	if processID <= 0 {
		return api.InvoicePreview{}, fail("invalid_input", fmt.Sprintf("invalid process ID %d", processID), 2, nil)
	}
	client, err := a.client()
	if err != nil {
		return api.InvoicePreview{}, err
	}
	preview, apiErr := client.InvoicePreview(processID)
	if apiErr != nil {
		return api.InvoicePreview{}, classifyAPI(apiErr)
	}
	return preview, nil
}
