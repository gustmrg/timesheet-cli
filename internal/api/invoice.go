package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gustmrg/timesheet-cli/internal/webparse"
)

type InvoiceQuery struct {
	Offset int
	Limit  int
	Search string
	Status string
}

type Invoice struct {
	ProcessID   int64  `json:"processId"`
	DeveloperID int64  `json:"developerId"`
	Developer   string `json:"developer"`
	Hours       string `json:"hours"`
	Amount      string `json:"amount"`
	Profit      string `json:"profit"`
	Deduction   string `json:"deduction"`
	CutDate     string `json:"cutDate"`
	Total       string `json:"total"`
	Status      string `json:"status"`
}

type InvoicePage struct {
	Invoices []Invoice `json:"invoices"`
	Offset   int       `json:"offset"`
	Returned int       `json:"returned"`
	Total    int       `json:"total"`
	Filtered int       `json:"filtered"`
}

type InvoicePreviewItem struct {
	Customer           string `json:"customer"`
	Project            string `json:"project"`
	MonetizedHours     string `json:"monetizedHours"`
	NonMonetizedHours  string `json:"nonMonetizedHours"`
	MonetizedAmount    string `json:"monetizedAmount"`
	NonMonetizedAmount string `json:"nonMonetizedAmount"`
}

type InvoicePreview struct {
	ProcessID int64                `json:"processId"`
	Items     []InvoicePreviewItem `json:"items"`
}

type invoiceRecord struct {
	ID                   int64  `json:"Id"`
	IDDeveloper          int64  `json:"IdDeveloper"`
	DeveloperName        string `json:"DeveloperName"`
	TotalTimeMonetize    string `json:"TotalTimeMonetize"`
	PayTotalTimeMonetize string `json:"PayTotalTimeMonetize"`
	Profit               string `json:"Profit"`
	Deduction            string `json:"Deduction"`
	CutDate              string `json:"CutDate"`
	Total                string `json:"Total"`
	InvoiceAttachment    string `json:"InvoiceAttachment"`
}

type invoiceDataTable struct {
	RecordsTotal    int             `json:"recordsTotal"`
	RecordsFiltered int             `json:"recordsFiltered"`
	Data            []invoiceRecord `json:"data"`
}

func (c *Client) ListInvoices(query InvoiceQuery) (InvoicePage, error) {
	return withReadRetry(c, func() (InvoicePage, error) {
		return c.listInvoices(query)
	})
}

func (c *Client) InvoicePreview(processID int64) (InvoicePreview, error) {
	return withReadRetry(c, func() (InvoicePreview, error) {
		form := url.Values{"idprocess": {strconv.FormatInt(processID, 10)}}
		var fragment string
		if err := c.readJSON(http.MethodPost, "/Manager/ReadPreviewNF", nil, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded; charset=UTF-8", true, &fragment); err != nil {
			return InvoicePreview{}, err
		}
		parsed, err := webparse.InvoicePreviewItems(strings.NewReader(fragment))
		if err != nil {
			return InvoicePreview{}, &Error{Kind: KindInvalidResponse, Message: err.Error(), Cause: err}
		}
		items := make([]InvoicePreviewItem, len(parsed))
		for index, item := range parsed {
			items[index] = InvoicePreviewItem{
				Customer: item.Customer, Project: item.Project,
				MonetizedHours: item.MonetizedHours, NonMonetizedHours: item.NonMonetizedHours,
				MonetizedAmount: item.MonetizedAmount, NonMonetizedAmount: item.NonMonetizedAmount,
			}
		}
		return InvoicePreview{ProcessID: processID, Items: items}, nil
	})
}

func (c *Client) listInvoices(query InvoiceQuery) (InvoicePage, error) {
	requestQuery := query
	if query.Status != "" {
		requestQuery.Offset = 0
		requestQuery.Limit = 9_999_999
	}
	form := invoiceDataTableForm(requestQuery)
	var response invoiceDataTable
	if err := c.readJSON(http.MethodPost, "/ManagerDeveloper/LoadDataTablesInvoice", nil, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded; charset=UTF-8", true, &response); err != nil {
		return InvoicePage{}, err
	}
	invoices := make([]Invoice, len(response.Data))
	for index, record := range response.Data {
		status := "sent"
		if strings.EqualFold(strings.TrimSpace(record.InvoiceAttachment), "Pendente") {
			status = "pending"
		}
		invoices[index] = Invoice{
			ProcessID: record.ID, DeveloperID: record.IDDeveloper, Developer: record.DeveloperName,
			Hours: record.TotalTimeMonetize, Amount: record.PayTotalTimeMonetize,
			Profit: record.Profit, Deduction: record.Deduction, CutDate: record.CutDate,
			Total: record.Total, Status: status,
		}
	}
	filtered := response.RecordsFiltered
	if query.Status != "" {
		matching := make([]Invoice, 0, len(invoices))
		for _, invoice := range invoices {
			if strings.EqualFold(invoice.Status, query.Status) {
				matching = append(matching, invoice)
			}
		}
		filtered = len(matching)
		start := min(query.Offset, len(matching))
		end := min(start+query.Limit, len(matching))
		invoices = matching[start:end]
	}
	return InvoicePage{
		Invoices: invoices, Offset: query.Offset, Returned: len(invoices),
		Total: response.RecordsTotal, Filtered: filtered,
	}, nil
}

func invoiceDataTableForm(query InvoiceQuery) url.Values {
	form := url.Values{
		"draw":             {"1"},
		"start":            {strconv.Itoa(query.Offset)},
		"length":           {strconv.Itoa(query.Limit)},
		"search[value]":    {query.Search},
		"search[regex]":    {"false"},
		"order[0][column]": {"5"},
		"order[0][dir]":    {"desc"},
	}
	columns := []struct {
		name      string
		orderable bool
	}{
		{"DeveloperName", true},
		{"TotalTimeMonetize", true},
		{"PayTotalTimeMonetize", true},
		{"Profit", false},
		{"Deduction", false},
		{"CutDate", true},
		{"Total", true},
		{"InvoiceAttachment", false},
	}
	for index, column := range columns {
		prefix := "columns[" + strconv.Itoa(index) + "]"
		form.Set(prefix+"[data]", column.name)
		form.Set(prefix+"[name]", "")
		form.Set(prefix+"[searchable]", "true")
		form.Set(prefix+"[orderable]", strconv.FormatBool(column.orderable))
		form.Set(prefix+"[search][value]", "")
		form.Set(prefix+"[search][regex]", "false")
	}
	return form
}
