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

	// 收集所有原始字符，这些是 Analyze 的输入
	// 但实际上 Analyze 接收的是 extractor 传过来的 chars，不是从 TextBoxes 中取的
	// TextBoxes 是 Analyze 的输出，所以这里只能通过重新调用 Analyze 来模拟
	// 让我换个思路：直接看原始 chars
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n======= 从 TextBoxes 收集 chars（已经是 Analyze 的输出）=======\n\n")

	// 收集所有 chars
	var allChars []model.Char
	for _, tb := range page.TextBoxes {
		for _, line := range tb.Lines {
			for _, ch := range line.Chars {
				allChars = append(allChars, ch)
			}
		}
	}

	// 按 SeqNo 排序
	sort.Slice(allChars, func(i, j int) bool {
		return allChars[i].SeqNo < allChars[j].SeqNo
	})

	// 只看 SeqNo > 0 的字符（排除 finalizeLine 插入的空格）
	fmt.Fprintf(&sb, "Total chars: %d\n\n", len(allChars))

	// 找到 SeqNo=81 附近的字符
	fmt.Fprintf(&sb, "=== 作者区字符 (SeqNo 80-180) ===\n")
	for _, ch := range allChars {
		if ch.SeqNo < 80 || ch.SeqNo > 180 {
			continue
		}
		fmt.Fprintf(&sb, "Seq=%3d Y=%7.2f X=%7.2f fs=%5.1f Text='%s'\n",
			ch.SeqNo, ch.Origin.Y, ch.Origin.X, ch.Font.Size, ch.Text)
	}

	os.WriteFile("output/debug_raw_input.txt", []byte(sb.String()), 0644)
	fmt.Println("Saved to output/debug_raw_input.txt")
}