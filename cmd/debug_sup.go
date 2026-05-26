//go:build ignore

package main

import (
	"fmt"
	"math"
	"os"
	"sort"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run debug_sup.go <file.pdf>")
		os.Exit(1)
	}

	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{ExtractText: true})
	result, err := e.ExtractFile(os.Args[1])
	if err != nil {
		panic(err)
	}

	page := result.Pages[0]

	// 收集所有字符
	var allChars []model.Char
	for _, tb := range page.TextBoxes {
		for _, l := range tb.Lines {
			allChars = append(allChars, l.Chars...)
		}
	}

	// 按 Y 降序、X 升序排序
	sort.Slice(allChars, func(i, j int) bool {
		dy := allChars[i].Origin.Y - allChars[j].Origin.Y
		if math.Abs(dy) > 1.0 {
			return dy > 0
		}
		return allChars[i].Origin.X < allChars[j].Origin.X
	})

	// 输出作者区域(Y 688-707) 每个字符的 X, Y, text, size
	fmt.Println("=== Author area chars (sorted by Y desc, X asc) ===")
	for _, c := range allChars {
		if c.Origin.Y >= 688 && c.Origin.Y <= 707 {
			fmt.Printf("  Y=%.2f X=%.1f Size=%.2f Text=%q\n",
				c.Origin.Y, c.Origin.X, c.Font.Size, c.Text)
		}
	}
}
