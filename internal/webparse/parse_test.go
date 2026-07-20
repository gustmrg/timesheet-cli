package webparse_test

import (
	"strings"
	"testing"

	"github.com/gustmrg/timesheet-cli/internal/webparse"
)

const worksheetHTML = `<!doctype html><html><body>
<table id="tbWorksheet"><tbody>
<tr>
  <td>ACME &amp; Co</td><td>Internal <strong>Tools</strong></td><td>Daily</td>
  <td>20/07/2026</td><td>09:00</td><td>10:30</td><td>01:30</td>
  <td><a id="123" name="Worksheet|456" title="Pré-aprovado">status</a></td>
  <td><a id="123">edit</a></td>
</tr>
</tbody></table>
<select class="form-control customer" name="customer">
  <option value="">Select</option><option value="10">ACME &amp; Co</option>
</select>
</body></html>`

func TestWorksheetPageExposesEntriesAndMetadata(t *testing.T) {
	entries, err := webparse.Entries(strings.NewReader(worksheetHTML))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.ID != 123 || got.EvaluateID == nil || *got.EvaluateID != 456 || got.Customer != "ACME & Co" || got.Project != "Internal Tools" || got.Status != "Pré-aprovado" {
		t.Fatalf("unexpected entry: %#v", got)
	}

	selects, err := webparse.Selects(strings.NewReader(worksheetHTML))
	if err != nil {
		t.Fatal(err)
	}
	if len(selects["customer"]) != 1 || selects["customer"][0].ID != 10 || selects["customer"][0].Name != "ACME & Co" {
		t.Fatalf("unexpected selects: %#v", selects)
	}
}

func TestEmptyAndMalformedWorksheetPagesAreSafe(t *testing.T) {
	for _, source := range []string{`<table id="tbWorksheet"><tbody></tbody></table>`, `<table id="tbWorksheet"><tr><td>unfinished`} {
		entries, err := webparse.Entries(strings.NewReader(source))
		if err != nil {
			t.Fatalf("Entries(%q): %v", source, err)
		}
		if len(entries) != 0 {
			t.Fatalf("Entries(%q) = %#v, want empty", source, entries)
		}
	}
}

func TestEntryWithoutEvaluationDoesNotUseActionTextAsStatus(t *testing.T) {
	html := `<table id="tbWorksheet"><tbody><tr>
<td>ACME</td><td>Internal</td><td>Daily</td><td>20/07/2026</td><td>09:00</td><td>10:00</td><td>01:00</td>
<td></td><td><a id="321">edit</a></td></tr></tbody></table>`
	entries, err := webparse.Entries(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != 321 || entries[0].EvaluateID != nil || entries[0].Status != "" {
		t.Fatalf("unexpected entry: %#v", entries)
	}
}

func TestLoginPageAndVerificationTokenAreDetected(t *testing.T) {
	html := `<form><input name="Login"><input type="password" name="Password"><input name="__RequestVerificationToken" value="a&amp;b"></form>`
	if !webparse.IsLoginPage([]byte(html)) {
		t.Fatal("login page was not detected")
	}
	if token := webparse.VerificationToken([]byte(html)); token != "a&b" {
		t.Fatalf("token = %q, want a&b", token)
	}
}

func TestDescriptionHTMLCanBeNormalizedToPlainText(t *testing.T) {
	got, err := webparse.FragmentText(`<p>First &amp; safe<br>next</p><p>Second</p>`)
	if err != nil {
		t.Fatal(err)
	}
	if got != "First & safe\nnext\n\nSecond" {
		t.Fatalf("FragmentText() = %q", got)
	}
}
