//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run test_merge.go <file.pdf>")
		os.Exit(1)
	}

	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{ExtractText: true})
	result, err := e.ExtractFile(os.Args[1])
	if err != nil {
		panic(err)
	}

	page := result.Pages[0]
	for i, tb := range page.TextBoxes {
		if i > 10 {
			break
		}
		fmt.Printf("TextBox %d (Y=%.1f-%.1f):\n", i, tb.BBox.Y0, tb.BBox.Y1)
		for j, l := range tb.Lines {
			text := ""
			for _, c := range l.Chars {
				text += c.Text
			}
			fmt.Printf("  Line %d (Y=%.1f, size=%.1f): %q\n", j, l.BBox.Y0, avgSize(l.Chars), text)
		}
	}
}

func avgSize(chars []model.Char) float64 {
	var sum float64
	var n float64
	for _, c := range chars {
		if c.Font.Size > 0 {
			sum += c.Font.Size
			n++
		}
	}
	if n > 0 {
		return sum / n
	}
	return 0
}
