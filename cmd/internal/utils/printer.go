package utils

import (
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/renderer"
	"github.com/olekukonko/tablewriter/tw"
)

// TablePrinter can be used to print data as a table
type TablePrinter struct {
	table *tablewriter.Table
}

// NewTablePrinter returns a new table printer
func NewTablePrinter() *TablePrinter {
	symbols := tw.NewSymbolCustom("Default").WithCenter("").WithColumn("").WithRow("")
	table := tablewriter.NewTable(os.Stdout,

		tablewriter.WithRenderer(renderer.NewBlueprint(tw.Rendition{
			Borders: tw.BorderNone,
			Symbols: symbols,
			Settings: tw.Settings{
				Lines:      tw.Lines{},
				Separators: tw.Separators{},
			},
		})),
		tablewriter.WithConfig(tablewriter.Config{
			Header: tw.CellConfig{
				Formatting: tw.CellFormatting{
					Alignment: tw.AlignLeft,
				},
			},
			Row: tw.CellConfig{
				Formatting: tw.CellFormatting{
					Alignment: tw.AlignLeft,
				},
			},
		}),
	)
	return &TablePrinter{
		table: table,
	}
}

// Print prints the table
func (t *TablePrinter) Print(headers []string, data [][]string) error {
	t.table.Header(headers)
	if err := t.table.Bulk(data); err != nil {
		return err
	}
	if err := t.table.Render(); err != nil {
		return err
	}
	return nil
}
