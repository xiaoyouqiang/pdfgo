package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

func main() {
	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{ExtractText: true})
	result, _ := e.ExtractFile("2605.23261v1.pdf")

	page := result.Pages[0]

	// Collect all chars from page 1
	var allChars []model.Char
	for _, tb := range page.TextBoxes {
		for _, line := range tb.Lines {
			for _, ch := range line.Chars {
				allChars = append(allChars, ch)
			}
		}
	}

	// Sort by SeqNo to get content stream order
	sort.Slice(allChars, func(i, j int) bool {
		return allChars[i].SeqNo < allChars[j].SeqNo
	})

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n======= PAGE 1 Characters in Author Area (Y 125-165) =======\n")
	fmt.Fprintf(&sb, "Total chars: %d\n\n", len(allChars))

	for i, ch := range allChars {
		// Filter to author area
		if ch.Origin.Y < 125 || ch.Origin.Y > 165 {
			continue
		}
		fmt.Fprintf(&sb, "[%3d] Seq=%3d Y=%7.2f X=%7.2f FontSize=%5.1f Text='%s'\n",
			i, ch.SeqNo, ch.Origin.Y, ch.Origin.X, ch.Font.Size, ch.Text)
	}

	os.WriteFile("output/debug_author_chars_raw.txt", []byte(sb.String()), 0644)
	fmt.Println("Saved to output/debug_author_chars_raw.txt")
}