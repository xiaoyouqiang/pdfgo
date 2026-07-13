package main

import (
	"fmt"
	"os"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

func main() {
	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{
		ExtractText:  true,
		ExtractTable: true,
	})
	result, err := e.ExtractFile(os.Args[1])
	if err != nil {
		fmt.Println("err:", err)
		return
	}

	// 找到包含 6.1.2 相关方 的页
	for _, p := range result.Pages {
		for _, t := range p.Tables {
			for _, row := range t.Cells {
				for _, cell := range row {
					if contains(cell.Text, "Customer") || contains(cell.Text, "顾客") {
						fmt.Printf("=== Found stakeholder table on page %d ===\n", p.PageNum)
						dumpPage(p)
						return
					}
				}
			}
		}
	}
	fmt.Println("No stakeholder table found")
	for _, p := range result.Pages {
		for _, tb := range p.TextBoxes {
			for _, l := range tb.Lines {
				t := l.Text()
				if contains(t, "Customer") || contains(t, "顾客") {
					fmt.Printf("Page %d TextBox has: %q\n", p.PageNum, t)
				}
			}
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func dumpPage(p model.Page) {
	fmt.Printf("Page %d: TextBoxes=%d Tables=%d\n", p.PageNum, len(p.TextBoxes), len(p.Tables))
	for ti, t := range p.Tables {
		fmt.Printf("\n--- Table %d: rows=%d cols=%d bbox=(%.1f,%.1f,%.1f,%.1f) ---\n",
			ti, t.Rows, t.Cols, t.BBox.X0, t.BBox.Y0, t.BBox.X1, t.BBox.Y1)
		for r, row := range t.Cells {
			for c, cell := range row {
				if cell.Text != "" || !cell.BBox.Empty() {
					fmt.Printf("  [%d][%d] bbox=(%.1f,%.1f,%.1f,%.1f) text=%q\n",
						r, c, cell.BBox.X0, cell.BBox.Y0, cell.BBox.X1, cell.BBox.Y1, cell.Text)
				}
			}
		}
	}
}
