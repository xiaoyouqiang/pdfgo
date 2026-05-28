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

	var sb strings.Builder

	for pi := 0; pi < 21; pi++ {
		page := result.Pages[pi]
		fmt.Fprintf(&sb, "\n======= PAGE %d TextBoxes (%d) =======\n", pi+1, len(page.TextBoxes))

		for i := range page.TextBoxes {
			tb := &page.TextBoxes[i]
			t := tb.Text()
			fmt.Fprintf(&sb, "TB[%2d] Y0=%7.1f Y1=%7.1f X0=%7.1f X1=%7.1f Lines=%2d\n",
				i, tb.BBox.Y0, tb.BBox.Y1, tb.BBox.X0, tb.BBox.X1, len(tb.Lines))
			fmt.Fprintf(&sb, "  Text:\n%s\n", t)
		}
	}

	os.WriteFile("output/textboxes_raw.txt", []byte(sb.String()), 0644)
	fmt.Println("Saved to output/textboxes_raw.txt")
}
