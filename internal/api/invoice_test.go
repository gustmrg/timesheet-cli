package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gustmrg/timesheet-cli/internal/api"
)

func TestClientListsInvoicesWithServerPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ManagerDeveloper/LoadDataTablesInvoice" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
			t.Error("invoice request is missing X-Requested-With")
		}
		if r.FormValue("start") != "20" || r.FormValue("length") != "10" || r.FormValue("search[value]") != "julho" {
			t.Errorf("unexpected pagination form: %v", r.Form)
		}
		if r.FormValue("columns[0][data]") != "DeveloperName" || r.FormValue("columns[7][data]") != "InvoiceAttachment" {
			t.Errorf("unexpected DataTables columns: %v", r.Form)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"draw":            1,
			"recordsTotal":    42,
			"recordsFiltered": 3,
			"data": []map[string]any{{
				"Id":                   901,
				"IdDeveloper":          77,
				"DeveloperName":        "Ana Lima",
				"TotalTimeMonetize":    "160:00",
				"PayTotalTimeMonetize": "R$ 12.000,00",
				"Profit":               "R$ 500,00",
				"Deduction":            "R$ 100,00",
				"CutDate":              "31/07/2026",
				"Total":                "R$ 12.400,00",
				"InvoiceAttachment":    "Pendente",
			}},
		})
	}))
	defer server.Close()

	client, err := api.New(api.Config{BaseURL: server.URL, SessionFile: filepath.Join(t.TempDir(), "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.ListInvoices(api.InvoiceQuery{Offset: 20, Limit: 10, Search: "julho"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Offset != 20 || page.Returned != 1 || page.Total != 42 || page.Filtered != 3 {
		t.Fatalf("unexpected page metadata: %#v", page)
	}
	if len(page.Invoices) != 1 {
		t.Fatalf("invoices = %d, want 1", len(page.Invoices))
	}
	got := page.Invoices[0]
	if got.ProcessID != 901 || got.DeveloperID != 77 || got.Developer != "Ana Lima" || got.Hours != "160:00" || got.Amount != "R$ 12.000,00" || got.Total != "R$ 12.400,00" || got.Status != "pending" {
		t.Fatalf("unexpected invoice: %#v", got)
	}
}

func TestClientFiltersInvoiceStatusBeforeLocalPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("start") != "0" || r.FormValue("length") != "9999999" || r.FormValue("search[value]") != "ana" {
			t.Errorf("unexpected status-filter request: %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"recordsTotal":    8,
			"recordsFiltered": 3,
			"data": []map[string]any{
				{"Id": 1, "DeveloperName": "Ana A", "InvoiceAttachment": "Pendente"},
				{"Id": 2, "DeveloperName": "Ana B", "InvoiceAttachment": "invoice.pdf"},
				{"Id": 3, "DeveloperName": "Ana C", "InvoiceAttachment": "Pendente"},
			},
		})
	}))
	defer server.Close()

	client, err := api.New(api.Config{BaseURL: server.URL, SessionFile: filepath.Join(t.TempDir(), "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.ListInvoices(api.InvoiceQuery{Offset: 1, Limit: 1, Search: "ana", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 8 || page.Filtered != 2 || page.Offset != 1 || page.Returned != 1 || len(page.Invoices) != 1 || page.Invoices[0].ProcessID != 3 {
		t.Fatalf("unexpected filtered page: %#v", page)
	}
}

func TestClientReturnsNormalizedInvoicePreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Manager/ReadPreviewNF" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Requested-With") != "XMLHttpRequest" || r.FormValue("idprocess") != "901" {
			t.Errorf("unexpected preview request: headers=%v form=%v", r.Header, r.Form)
		}
		_ = json.NewEncoder(w).Encode(`<table id="tbWorksheet"><thead><tr>
<th>Cliente</th><th>Projeto</th><th>Total Contabilizado</th><th>Total Não Contabilizado</th><th>Valor Total</th><th>Valor Total Não Contabilizado</th>
</tr></thead><tbody><tr>
<td>ACME &amp; Co</td><td>Internal Tools</td><td>120:00</td><td>08:00</td><td>R$ 9.000,00</td><td>R$ 600,00</td>
</tr></tbody></table>`)
	}))
	defer server.Close()

	client, err := api.New(api.Config{BaseURL: server.URL, SessionFile: filepath.Join(t.TempDir(), "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := client.InvoicePreview(901)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ProcessID != 901 || len(preview.Items) != 1 {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	got := preview.Items[0]
	if got.Customer != "ACME & Co" || got.Project != "Internal Tools" || got.MonetizedHours != "120:00" || got.NonMonetizedHours != "08:00" || got.MonetizedAmount != "R$ 9.000,00" || got.NonMonetizedAmount != "R$ 600,00" {
		t.Fatalf("unexpected preview item: %#v", got)
	}
}
