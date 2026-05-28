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

	page := result.Pages[0]

	var sb strings.Builder

	fmt.Fprintf(&sb, "\n======= Go vs PyMuPDF 作者块对比 =======\n\n")

	// Go: TB[2] 作者块
	for i := 0; i < len(page.TextBoxes); i++ {
		tb := &page.TextBoxes[i]
		if tb.BBox.Y0 < 120 || tb.BBox.Y0 > 180 {
			continue
		}

		fmt.Fprintf(&sb, "Go TB[%d] Y0=%.1f Y1=%.1f X0=%.1f X1=%.1f Lines=%d\n",
			i, tb.BBox.Y0, tb.BBox.Y1, tb.BBox.X0, tb.BBox.X1, len(tb.Lines))

		for j, line := range tb.Lines {
			// 统计每种字号
			sizeCount := make(map[float64]int)
			for _, ch := range line.Chars {
				sizeCount[ch.Font.Size]++
			}
			sizeInfo := ""
			for s, c := range sizeCount {
				sizeInfo += fmt.Sprintf("  size=%.0f(%dchars)", s, c)
			}

			fmt.Fprintf(&sb, "  Go Line[%d] Y0=%.1f Y1=%.1f X0=%.1f X1=%.1f chars=%d%s\n",
				j, line.BBox.Y0, line.BBox.Y1, line.BBox.X0, line.BBox.X1, len(line.Chars), sizeInfo)

			// 显示前几个字符
			preview := ""
			for k, ch := range line.Chars {
				if k >= 30 {
					preview += "..."
					break
				}
				preview += ch.Text
			}
			fmt.Fprintf(&sb, "    Text: %s\n", preview)
		}

		fmt.Fprintf(&sb, "\nPyMuPDF Block 1 (同区域):\n")
		fmt.Fprintf(&sb, "  Lines: 2\n")
		fmt.Fprintf(&sb, "  Line 0: Y=127.8-143.9  Text=Yuanyuan Wang1, Dongchao Yang1, Yayue Deng1,\n")
		fmt.Fprintf(&sb, "    (包含 size=12 主文本 + size=8 上标，全部在同一行)\n")
		fmt.Fprintf(&sb, "  Line 1: Y=141.9-158.0  Text=Zhiyong Wu1,2,†, Yiwen Guo3, Helen Meng1, Xixin Wu1,†,\n")
		fmt.Fprintf(&sb, "    (包含 size=12 主文本 + size=8 上标，全部在同一行)\n")
	}

	os.WriteFile("output/debug_pen_tracking.txt", []byte(sb.String()), 0644)
	fmt.Println("Saved to output/debug_pen_tracking.txt")
}