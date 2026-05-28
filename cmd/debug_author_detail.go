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

	fmt.Fprintf(&sb, "\n======= PAGE 1 TextBoxes Detail =======\n")
	fmt.Fprintf(&sb, "Page size: %.1f x %.1f\n\n", page.Width, page.Height)

	for i := 0; i < len(page.TextBoxes); i++ {
		tb := &page.TextBoxes[i]
		t := tb.Text()
		if len(t) == 0 {
			continue
		}

		// Only show textboxes in the author area (Y around 120-180)
		if tb.BBox.Y0 < 120 || tb.BBox.Y0 > 180 {
			continue
		}

		fmt.Fprintf(&sb, "TB[%2d] Y0=%7.1f Y1=%7.1f X0=%7.1f X1=%7.1f Lines=%2d\n",
			i, tb.BBox.Y0, tb.BBox.Y1, tb.BBox.X0, tb.BBox.X1, len(tb.Lines))
		fmt.Fprintf(&sb, "  Text: %s\n", t)
		fmt.Fprintf(&sb, "  Line BBoxes:\n")

		for j, line := range tb.Lines {
			fmt.Fprintf(&sb, "    Line[%d] Y0=%7.1f Y1=%7.1f X0=%7.1f X1=%7.1f\n",
				j, line.BBox.Y0, line.BBox.Y1, line.BBox.X0, line.BBox.X1)

			// Show first char of each line
			if len(line.Chars) > 0 {
				first := line.Chars[0]
				last := line.Chars[len(line.Chars)-1]
				fmt.Fprintf(&sb, "      First char: '%s' Origin=(%.1f, %.1f) FontSize=%.1f\n",
					first.Text, first.Origin.X, first.Origin.Y, first.Font.Size)
				fmt.Fprintf(&sb, "      Last char: '%s' Origin=(%.1f, %.1f)\n",
					last.Text, last.Origin.X, last.Origin.Y)
			}
		}
		fmt.Fprintf(&sb, "\n")
	}

	os.WriteFile("output/debug_author_detail.txt", []byte(sb.String()), 0644)
	fmt.Println("Saved to output/debug_author_detail.txt")
}