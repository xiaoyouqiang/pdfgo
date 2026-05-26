//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(1)
	}
	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{ExtractText: true})
	result, err := e.ExtractFile(os.Args[1])
	if err != nil {
		panic(err)
	}

	page := result.Pages[0]
	for _, tb := range page.TextBoxes {
		for _, l := range tb.Lines {
			for _, c := range l.Chars {
				for _, r := range c.Text {
					if r > 127 && r != 0x2020 && r != ' ' {
						fmt.Printf("Rune=U+%04X Text=%q X=%.1f Y=%.1f Font=%s Size=%.2f\n",
							r, c.Text, c.Origin.X, c.Origin.Y, c.Font.Name, c.Font.Size)
					}
				}
			}
		}
	}
}
