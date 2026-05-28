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
	fmt.Fprintf(&sb, "=== 标题字符 (Y 70-120) ===\n\n")

	// 模拟 pen-tracking
	penX := 0.0
	penY := 0.0
	penFontSize := 0.0
	startX := 0.0
	initialized := false
	lineNum := 0
	blockNum := 0

	for _, ch := range allChars {
		if ch.Origin.Y < 70 || ch.Origin.Y > 120 {
			continue
		}

		fs := ch.Font.Size
		if fs <= 0 {
			fs = 14.3
		}

		if !initialized {
			penX = ch.Origin.X + ch.Advance
			penY = ch.Origin.Y
			penFontSize = fs
			startX = ch.Origin.X
			initialized = true
			blockNum = 0
			lineNum = 0
			fmt.Fprintf(&sb, "[Block %d Line %d] FIRST '%s' Y=%.2f X=%.2f fs=%.1f\n",
				blockNum, lineNum, ch.Text, ch.Origin.Y, ch.Origin.X, fs)
			continue
		}

		dx := ch.Origin.X - penX
		dy := ch.Origin.Y - penY
		normSize := penFontSize
		if normSize <= 0 {
			normSize = fs
		}
		baseOffset := dy / normSize
		spacing := dx / normSize
		absBase := baseOffset
		if absBase < 0 {
			absBase = -absBase
		}

		newPara := false
		newLine := false
		reason := ""

		if absBase < 0.8 {
			reason = "same-baseline"
		} else if absBase <= 1.5 {
			reason = "new-line"
			newLine = true
			indent := ch.Origin.X - startX
			if indent > 0.5 {
				newPara = true
				reason = "new-PARA (indent)"
			}
		} else {
			reason = "new-PARA"
			newPara = true
			newLine = true
		}

		if newPara {
			blockNum++
			lineNum = 0
			fmt.Fprintf(&sb, "\n")
		} else if newLine {
			lineNum++
			fmt.Fprintf(&sb, "\n")
		}

		fmt.Fprintf(&sb, "[Block %d Line %d] '%s' Y=%.2f dy=%.2f penFs=%.1f baseOff=%.2f spacing=%.2f indent=%.1f -> %s\n",
			blockNum, lineNum, ch.Text, ch.Origin.Y, dy, penFontSize, baseOffset, spacing, ch.Origin.X-startX, reason)

		penX = ch.Origin.X + ch.Advance
		penY = ch.Origin.Y
		penFontSize = fs
		if newLine || lineNum == 0 {
			startX = ch.Origin.X
		}
	}

	os.WriteFile("output/debug_title.txt", []byte(sb.String()), 0644)
	fmt.Println("Saved to output/debug_title.txt")
}