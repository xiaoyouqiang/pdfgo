//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/table"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: debug_cells <file.pdf>")
		os.Exit(1)
	}

	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{
		ExtractText:  false,
		ExtractTable: true,
		ExtractImage: false,
	})
	result, err := e.ExtractFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	f, _ := os.Create("debug_cells.txt")
	defer f.Close()

	for pi, page := range result.Pages {
		fmt.Fprintf(f, "=== Page %d (W=%.1f H=%.1f) ===\n", pi+1, page.Width, page.Height)
		settings := table.DefaultSettings()
		settings.PageWidth = page.Width
		settings.PageHeight = page.Height
		fmt.Fprintf(f, "Tables: %d\n", len(page.Tables))
		for i, tbl := range page.Tables {
			fmt.Fprintf(f, "Table[%d]: rows=%d cols=%d bbox=(%.1f,%.1f,%.1f,%.1f)\n", i, tbl.Rows, tbl.Cols, tbl.BBox.X0, tbl.BBox.Y0, tbl.BBox.X1, tbl.BBox.Y1)
			for r := 0; r < tbl.Rows; r++ {
				for c := 0; c < tbl.Cols; c++ {
					cell := tbl.Cells[r][c]
					if !cell.BBox.Empty() {
						fmt.Fprintf(f, "  [%d][%d]: X0=%.2f Y0=%.2f X1=%.2f Y1=%.2f text=%q\n", r, c, cell.BBox.X0, cell.BBox.Y0, cell.BBox.X1, cell.BBox.Y1, cell.Text)
					}
				}
			}
		}
	}
	fmt.Println("Output written to debug_cells.txt")
}
