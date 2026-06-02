package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
)

func main() {
	ext := pdfextract.NewExtractor(pdfextract.ExtractionOptions{
		ExtractText:  true,
		ExtractTable: true,
	})
	result, err := ext.ExtractFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	type tbInfo struct {
		Page   int     `json:"page"`
		Index  int     `json:"index"`
		X0     float64 `json:"x0"`
		Y0     float64 `json:"y0"`
		X1     float64 `json:"x1"`
		Y1     float64 `json:"y1"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
		Lines  int     `json:"lines"`
		Text   string  `json:"text"`
	}

	var all []tbInfo
	for pi, p := range result.Pages {
		for i, tb := range p.TextBoxes {
			w := tb.BBox.X1 - tb.BBox.X0
			h := tb.BBox.Y1 - tb.BBox.Y0
			all = append(all, tbInfo{
				Page:   pi + 1,
				Index:  i,
				X0:     tb.BBox.X0,
				Y0:     tb.BBox.Y0,
				X1:     tb.BBox.X1,
				Y1:     tb.BBox.Y1,
				Width:  w,
				Height: h,
				Lines:  len(tb.Lines),
				Text:   tb.Text(),
			})
		}
	}

	data, _ := json.MarshalIndent(all, "", "  ")
	if err := os.WriteFile(os.Args[2], data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Write error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Wrote %d TextBoxes to %s\n", len(all), os.Args[2])
}
