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

	page := result.Pages[4] // Page 5 (0-indexed)

	var allChars []model.Char
	for _, tb := range page.TextBoxes {
		for _, line := range tb.Lines {
			for _, ch := range line.Chars {
				allChars = append(allChars, ch)
			}
		}
	}
	sort.Slice(allChars, func(i, j int) bool {
		return allChars[i].SeqNo < allChars[j].SeqNo
	})

	var sb strings.Builder
	fmt.Fprintf(&sb, "=== 公式区字符 (Y 145-200, X 300-530) ===\n\n")

	penX := 0.0
	penY := 0.0
	penFontSize := 0.0
	initialized := false
	blockNum := 0

	for _, ch := range allChars {
		if ch.Origin.Y < 145 || ch.Origin.Y > 200 || ch.Origin.X < 300 || ch.Origin.X > 530 {
			continue
		}
		fs := ch.Font.Size
		if fs <= 0 {
			fs = 12
		}

		if !initialized {
			penX = ch.Origin.X + ch.Advance
			penY = ch.Origin.Y
			penFontSize = fs
			initialized = true
			fmt.Fprintf(&sb, "[Block %d] FIRST '%s' Y=%.2f X=%.2f fs=%.0f\n", blockNum, ch.Text, ch.Origin.Y, ch.Origin.X, fs)
			continue
		}

		dx := ch.Origin.X - penX
		dy := ch.Origin.Y - penY
		normSize := penFontSize
		if normSize <= 0 {
			normSize = fs
		}
		baseOffset := dy / normSize
		absBase := baseOffset
		if absBase < 0 {
			absBase = -absBase
		}
		spacing := dx / normSize

		newPara := false
		newLine := false
		reason := ""

		if absBase < 0.8 {
			reason = fmt.Sprintf("same-base(abs=%.2f)", absBase)
		} else if absBase <= 1.5 {
			reason = fmt.Sprintf("new-line(abs=%.2f)", absBase)
			newLine = true
		} else {
			reason = fmt.Sprintf("new-PARA(abs=%.2f>1.5)", absBase)
			newPara = true
			newLine = true
		}

		if newPara {
			blockNum++
			fmt.Fprintf(&sb, "\n")
		} else if newLine {
			fmt.Fprintf(&sb, "\n")
		}

		fmt.Fprintf(&sb, "[Block %d] '%s' Y=%.2f X=%.2f fs=%.0f dy=%.2f penFs=%.0f sp=%.1f -> %s\n",
			blockNum, ch.Text, ch.Origin.Y, ch.Origin.X, fs, dy, penFontSize, spacing, reason)

		penX = ch.Origin.X + ch.Advance
		penY = ch.Origin.Y
		penFontSize = fs
	}

	os.WriteFile("output/debug_formula.txt", []byte(sb.String()), 0644)
	fmt.Println("Saved to output/debug_formula.txt")
}