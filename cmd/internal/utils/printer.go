package utils

import (
	"os"

	"github.com/olekukonko/tablewriter"
)

// TablePrinter can be used to print data as a table
type TablePrinter struct {
	table *tablewriter.Table
}

// NewTablePrinter returns a new table printer
func NewTablePrinter() *TablePrinter {
	table := tablewriter.NewWriter(os.Stdout)

	table.SetHeaderLine(false)
	table.SetAutoWrapText(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetBorder(false)
	table.SetCenterSeparator("")
	table.SetColumnSeparator("")
	table.SetRowSeparator("")
	table.SetRowLine(false)
	table.SetTablePadding("\t") // pad with tabs
	table.SetNoWhiteSpace(true) // no whitespace in front of every line

	return &TablePrinter{
		table: table,
	}
}

// Print prints the table
func (t *TablePrinter) Print(headers []string, data [][]string) {
	t.table.SetHeader(headers)
	t.table.AppendBulk(data)
	t.table.Render()
}
