package main

import (
	"fmt"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"os"
	"strings"
)

func main() {
	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{ExtractText: true})
	result, _ := e.ExtractFile("2605.23261v1.pdf")

	// Check page 1 (index 0)
	page := result.Pages[0]

	var sb strings.Builder

	fmt.Fprintf(&sb, "\n======= PAGE 1 Characters (first 200) =======\n")
	fmt.Fprintf(&sb, "Page size: %.1f x %.1f\n\n", page.Width, page.Height)

	for i := 0; i < 200 && i < len(page.TextBoxes); i++ {
		tb := &page.TextBoxes[i]
		t := tb.Text()
		if len(t) == 0 {
			continue
		}

		// Only show textboxes in the author area (Y around 120-170)
		if tb.BBox.Y0 < 120 || tb.BBox.Y0 > 180 {
			continue
		}

		fmt.Fprintf(&sb, "TB[%2d] Y0=%7.1f Y1=%7.1f X0=%7.1f X1=%7.1f Lines=%2d\n",
			i, tb.BBox.Y0, tb.BBox.Y1, tb.BBox.X0, tb.BBox.X1, len(tb.Lines))

		for j, line := range tb.Lines {
			fmt.Fprintf(&sb, "  Line[%d] Y0=%7.1f Y1=%7.1f: %s\n",
				j, line.BBox.Y0, line.BBox.Y1, line.Text())
		}
	}

	os.WriteFile("output/debug_author_chars.txt", []byte(sb.String()), 0644)
	fmt.Println("Saved to output/debug_author_chars.txt")
}