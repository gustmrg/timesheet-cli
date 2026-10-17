package cli

import (
	"fmt"
	"io"
	"strconv"

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
	var options invoiceListOptions
	cmd := &cobra.Command{Use: "list", Short: "List invoices", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		page, err := a.listInvoices(options)
		if err != nil {
			return err
		}
		return a.success(page, func() {
			printInvoiceTable(a.out, page.Invoices)
			if remaining := page.Filtered - page.Offset - page.Returned; remaining > 0 {
				fmt.Fprintf(a.out, "… %d more\n", remaining)
			}
		})
	}}
	cmd.Flags().BoolVar(&options.all, "all", false, "show every matching invoice")
	cmd.Flags().IntVar(&options.limit, "limit", 20, "maximum number of invoices")
	cmd.Flags().IntVar(&options.offset, "offset", 0, "number of matching invoices to skip")
	cmd.Flags().StringVar(&options.search, "search", "", "search invoice records")
	cmd.Flags().StringVar(&options.status, "status", "", "filter by status: pending or sent")
	return cmd
}

func (a *app) invoicePreviewCommand() *cobra.Command {
	return &cobra.Command{Use: "preview <process-id>", Short: "Show an invoice process breakdown", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		processID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || processID <= 0 {
			return fail("invalid_input", fmt.Sprintf("invalid process ID %q", args[0]), 2, err)
		}
		preview, err := a.invoicePreview(processID)
		if err != nil {
			return err
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
