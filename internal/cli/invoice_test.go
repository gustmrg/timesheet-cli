package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInvoiceListExposesNormalizedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ManagerDeveloper/LoadDataTablesInvoice" {
			http.NotFound(w, r)
			return
		}
		if r.FormValue("start") != "10" || r.FormValue("length") != "5" || r.FormValue("search[value]") != "Ana" {
			t.Errorf("unexpected invoice query: %v", r.Form)
		}
		writeCLIJSON(w, map[string]any{
			"recordsTotal": 12, "recordsFiltered": 1,
			"data": []map[string]any{{
				"Id": 901, "IdDeveloper": 77, "DeveloperName": "Ana Lima",
				"TotalTimeMonetize": "160:00", "PayTotalTimeMonetize": "R$ 12.000,00",
				"CutDate": "31/07/2026", "Total": "R$ 12.400,00", "InvoiceAttachment": "Pendente",
			}},
		})
	}))
	defer server.Close()
	configure(t, server.URL)

	code, stdout, stderr := execute(t, []string{"invoice", "list", "--limit", "5", "--offset", "10", "--search", "Ana", "--json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Invoices []struct {
				ProcessID int64  `json:"processId"`
				Status    string `json:"status"`
			} `json:"invoices"`
			Offset, Returned, Total, Filtered int
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.Offset != 10 || envelope.Data.Returned != 1 || envelope.Data.Total != 12 || envelope.Data.Filtered != 1 || len(envelope.Data.Invoices) != 1 || envelope.Data.Invoices[0].ProcessID != 901 || envelope.Data.Invoices[0].Status != "pending" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestInvoiceListPrintsFinancialColumnsAndPaginationHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeCLIJSON(w, map[string]any{
			"recordsTotal": 3, "recordsFiltered": 3,
			"data": []map[string]any{{
				"Id": 901, "DeveloperName": "Ana Lima", "CutDate": "31/07/2026",
				"TotalTimeMonetize": "160:00", "PayTotalTimeMonetize": "R$ 12.000,00",
				"Profit": "R$ 500,00", "Deduction": "R$ 100,00", "Total": "R$ 12.400,00",
				"InvoiceAttachment": "Pendente",
			}},
		})
	}))
	defer server.Close()
	configure(t, server.URL)

	code, stdout, stderr := execute(t, []string{"invoice", "list", "--limit", "1"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	for _, fragment := range []string{"PROCESS", "PROFIT", "DEDUCTION", "R$ 500,00", "R$ 100,00", "… 2 more"} {
		if !strings.Contains(stdout, fragment) {
			t.Errorf("output %q missing %q", stdout, fragment)
		}
	}
}

func TestInvoicePreviewExposesNormalizedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Manager/ReadPreviewNF" || r.FormValue("idprocess") != "901" {
			http.NotFound(w, r)
			return
		}
		writeCLIJSON(w, `<table id="tbWorksheet"><tbody><tr>
<td>ACME</td><td>Internal</td><td>120:00</td><td>08:00</td><td>R$ 9.000,00</td><td>R$ 600,00</td>
</tr></tbody></table>`)
	}))
	defer server.Close()
	configure(t, server.URL)

	code, stdout, stderr := execute(t, []string{"invoice", "preview", "901", "--json"}, "")
	if code != 0 || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			ProcessID int64 `json:"processId"`
			Items     []struct {
				Customer       string `json:"customer"`
				Project        string `json:"project"`
				MonetizedHours string `json:"monetizedHours"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Data.ProcessID != 901 || len(envelope.Data.Items) != 1 || envelope.Data.Items[0].Customer != "ACME" || envelope.Data.Items[0].Project != "Internal" || envelope.Data.Items[0].MonetizedHours != "120:00" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestInvoicePreviewPrintsReadableEmptyState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeCLIJSON(w, `<table id="tbWorksheet"><tbody></tbody></table>`)
	}))
	defer server.Close()
	configure(t, server.URL)

	code, stdout, stderr := execute(t, []string{"invoice", "preview", "901"}, "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "process 901") || !strings.Contains(stdout, "no invoice items") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}
