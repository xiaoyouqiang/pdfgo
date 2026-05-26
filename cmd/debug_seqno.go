//go:build ignore

package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run debug_seqno.go <file.pdf>")
		os.Exit(1)
	}

	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{ExtractText: true})
	result, err := e.ExtractFile(os.Args[1])
	if err != nil {
		panic(err)
	}

	page := result.Pages[0]
	var allChars []model.Char
	for _, tb := range page.TextBoxes {
		for _, l := range tb.Lines {
			allChars = append(allChars, l.Chars...)
		}
	}

	// Sort by SeqNo
	sort.Slice(allChars, func(i, j int) bool {
		return allChars[i].SeqNo < allChars[j].SeqNo
	})

	// Show chars in author/title area by SeqNo
	fmt.Println("=== Chars sorted by SeqNo (Y 680-770) ===")
	for _, c := range allChars {
		if c.Origin.Y >= 640 c.Origin.Y >= 680 && c.Origin.Y <= 770c.Origin.Y >= 680 && c.Origin.Y <= 770 c.Origin.Y <= 770 {
			fmt.Printf("  SeqNo=%3d Y=%.2f X=%.1f Size=%.2f Text=%q\n",
				c.SeqNo, c.Origin.Y, c.Origin.X, c.Font.Size, c.Text)
		}
	}
}
