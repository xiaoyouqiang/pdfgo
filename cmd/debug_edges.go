package main

import (
	"fmt"
	"os"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/table"
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

	// 用 table.Detect 重新检测，看页 4 的表结构
	settings := table.DefaultSettings()
	fmt.Printf("Default settings: %+v\n", settings)

	allTables := table.Detect(result.InterpretResult, settings)
	fmt.Printf("Total tables detected: %d\n", len(allTables))
	for i, t := range allTables {
		fmt.Printf("  Table %d: rows=%d cols=%d bbox=(%.2f,%.2f,%.2f,%.2f)\n",
			i, t.Rows, t.Cols, t.BBox.X0, t.BBox.Y0, t.BBox.X1, t.BBox.Y1)
	}
}

