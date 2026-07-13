package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// Dumps char-level details for lines containing any of the given needles.
// Usage: go run ./cmd/debug_chars.go -i <input.pdf> needle1 [needle2 ...]
func main() {
	var inputFile string
	var needles []string
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "-i" && i+1 < len(os.Args) {
			i++
			inputFile = os.Args[i]
		} else {
			needles = append(needles, os.Args[i])
		}
	}
	if inputFile == "" || len(needles) == 0 {
		fmt.Println("Usage: debug_chars -i <input.pdf> needle1 [needle2 ...]")
		os.Exit(1)
	}
	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{
		ExtractText:  true,
		ExtractTable: true,
	})
	result, err := e.ExtractFile(inputFile)
	if err != nil {
		panic(err)
	}
	for pgIdx, page := range result.Pages {
		for tbIdx, tb := range page.TextBoxes {
			for lineIdx, line := range tb.Lines {
				text := line.Text()
				matched := false
				for _, n := range needles {
					if strings.Contains(text, n) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
				fmt.Printf("=== page %d tb %d line %d: %q\n", pgIdx+1, tbIdx, lineIdx, text)
				for ci, c := range line.Chars {
					fmt.Printf("  [%2d] text=%-4q x=%8.2f adv=%6.2f seq=%4d font=%q\n",
						ci, c.Text, c.Origin.X, c.Advance, c.SeqNo, c.Font.Name)
				}
			}
		}
	}
	_ = model.Page{}
}
