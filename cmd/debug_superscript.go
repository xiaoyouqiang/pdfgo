package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

import (
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

func main() {
	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{ExtractText: true})
	result, _ := e.ExtractFile("2605.23261v1.pdf")

	page := result.Pages[0]

	// 收集所有字符，按 SeqNo 排序
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
	fmt.Fprintf(&sb, "\n======= 模拟 Pen-Tracking 逐字符处理 =======\n\n")

	// 模拟 pen-tracking，只追踪 Y 在 120-170 之间的字符
	penX := 0.0
	penY := 0.0
	penFontSize := 0.0
	initialized := false
	lineNum := 0

	for _, ch := range allChars {
		if ch.Origin.Y < 120 || ch.Origin.Y > 170 {
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
			lineNum = 0
			fmt.Fprintf(&sb, "[Line %d] FIRST: '%s' Y=%.2f X=%.2f fs=%.0f\n",
				lineNum, ch.Text, ch.Origin.Y, ch.Origin.X, fs)
			continue
		}

		dx := ch.Origin.X - penX
		dy := ch.Origin.Y - penY

		// 用 penFontSize 归一化
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

		newLine := false
		reason := ""
		if absBase < 0.8 {
			reason = fmt.Sprintf("same line (absBase=%.2f<0.8)", absBase)
		} else if absBase <= 1.5 {
			reason = fmt.Sprintf("NEW LINE (0.8<absBase=%.2f<=1.5)", absBase)
			newLine = true
		} else {
			reason = fmt.Sprintf("NEW PARA (absBase=%.2f>1.5)", absBase)
			newLine = true
		}

		if newLine {
			lineNum++
			fmt.Fprintf(&sb, "\n")
		}

		fmt.Fprintf(&sb, "[Line %d] '%s' Y=%.2f dy=%.2f penFs=%.0f spacing=%.2f -> %s\n",
			lineNum, ch.Text, ch.Origin.Y, dy, penFontSize, spacing, reason)

		penX = ch.Origin.X + ch.Advance
		penY = ch.Origin.Y
		penFontSize = fs
	}

	os.WriteFile("output/debug_superscript.txt", []byte(sb.String()), 0644)
	fmt.Println("Saved to output/debug_superscript.txt")
}