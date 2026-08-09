package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gustmrg/timesheet-cli/internal/api"
	"github.com/spf13/cobra"
)

const allInvoiceRows = 9_999_999

func (a *app) invoiceCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "invoice", Short: "Inspect invoices", Args: cobra.NoArgs}
	cmd.AddCommand(a.invoiceListCommand(), a.invoicePreviewCommand())
	return cmd
}

func (a *app) invoiceListCommand() *cobra.Command {
	var all bool
	var limit, offset int
	var search, status string
	cmd := &cobra.Command{Use: "list", Short: "List invoices", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		if limit <= 0 {
			return fail("invalid_input", "--limit must be a positive integer", 2, nil)
		}
		if offset < 0 {
			return fail("invalid_input", "--offset cannot be negative", 2, nil)
		}
		status = strings.ToLower(strings.TrimSpace(status))
		if status != "" && status != "pending" && status != "sent" {
			return fail("invalid_input", "--status must be `pending` or `sent`", 2, nil)
		}
		if all {
			offset = 0
			limit = allInvoiceRows
		}
		client, err := a.client()
		if err != nil {
			return err
		}
		page, apiErr := client.ListInvoices(api.InvoiceQuery{Offset: offset, Limit: limit, Search: search, Status: status})
		if apiErr != nil {
			return classifyAPI(apiErr)
		}
		return a.success(page, func() {
			printInvoiceTable(a.out, page.Invoices)
			if remaining := page.Filtered - page.Offset - page.Returned; remaining > 0 {
				fmt.Fprintf(a.out, "… %d more\n", remaining)
			}
		})
	}}
	cmd.Flags().BoolVar(&all, "all", false, "show every matching invoice")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of invoices")
	cmd.Flags().IntVar(&offset, "offset", 0, "number of matching invoices to skip")
	cmd.Flags().StringVar(&search, "search", "", "search invoice records")
	cmd.Flags().StringVar(&status, "status", "", "filter by status: pending or sent")
	return cmd
}

func (a *app) invoicePreviewCommand() *cobra.Command {
	return &cobra.Command{Use: "preview <process-id>", Short: "Show an invoice process breakdown", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		processID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || processID <= 0 {
			return fail("invalid_input", fmt.Sprintf("invalid process ID %q", args[0]), 2, err)
		}
		client, clientErr := a.client()
		if clientErr != nil {
			return clientErr
		}
		preview, apiErr := client.InvoicePreview(processID)
		if apiErr != nil {
			return classifyAPI(apiErr)
		}
		return a.success(preview, func() {
			fmt.Fprintf(a.out, "process %d\n", processID)
			printInvoicePreviewTable(a.out, preview.Items)
		})
	}}
}

func printInvoiceTable(w io.Writer, invoices []api.Invoice) {
	head := []string{"PROCESS", "CUT DATE", "DEVELOPER", "HOURS", "AMOUNT", "PROFIT", "DEDUCTION", "TOTAL", "STATUS"}
	rows := make([][]string, len(invoices))
	for index, invoice := range invoices {
		rows[index] = []string{
			strconv.FormatInt(invoice.ProcessID, 10), invoice.CutDate, invoice.Developer,
			invoice.Hours, invoice.Amount, invoice.Profit, invoice.Deduction, invoice.Total, invoice.Status,
		}
	}
	printTable(w, head, rows)
}

func printInvoicePreviewTable(w io.Writer, items []api.InvoicePreviewItem) {
	if len(items) == 0 {
		fmt.Fprintln(w, "no invoice items")
		return
	}
	head := []string{"CUSTOMER", "PROJECT", "MONETIZED HOURS", "NON-MONETIZED HOURS", "MONETIZED AMOUNT", "NON-MONETIZED AMOUNT"}
	rows := make([][]string, len(items))
	for index, item := range items {
		rows[index] = []string{
			item.Customer, item.Project, item.MonetizedHours, item.NonMonetizedHours,
			item.MonetizedAmount, item.NonMonetizedAmount,
		}
	}
	printTable(w, head, rows)
}
