package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	// 读取Go输出
	goData, _ := os.ReadFile("output/textboxes_raw.txt")
	
	// 读取PyMuPDF输出
	pymData, _ := os.ReadFile("output/pymupdf_output.txt")

	// Go TB[5] 内容
	pageMarker := "======= PAGE 5 TextBoxes"
	idx := strings.Index(string(goData), pageMarker)
	pageEnd := "======= PAGE 6 TextBoxes"
	idxEnd := strings.Index(string(goData)[idx:], pageEnd)
	content := string(goData)[idx : idx+idxEnd]
	
	lines := strings.Split(content, "\n")
	tbIdx := -1
	var goTB5 string
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "TB[") {
			tbIdx++
		}
		if tbIdx == 5 {
			if strings.HasPrefix(line, "  Text:") {
				goTB5 = line[7:]
			} else if len(strings.TrimSpace(line)) > 0 && !strings.HasPrefix(strings.TrimSpace(line), "TB[") && !strings.HasPrefix(strings.TrimSpace(line), "Text:") {
				goTB5 += " " + strings.TrimSpace(line)
			}
		}
	}

	fmt.Println("=== Go TB[5] 内容 ===")
	fmt.Println(goTB5[:200] + "...")
	
	// PyMuPDF Block 4 内容
	pymIdx := strings.Index(string(pymData), "\r\nPAGE 5\r\n")
	pymIdxEnd := strings.Index(string(pymData)[pymIdx:], "\r\n============================================================\r\nPAGE 6\r\n")
	pymContent := string(pymData)[pymIdx : pymIdx+pymIdxEnd]
	
	fmt.Println()
	fmt.Println("=== PyMuPDF Block 4 内容 ===")
	// 找 Block 4
	blockIdx := 0
	for strings.Contains(pymContent, "--- Block ") {
		blkStart := strings.Index(pymContent, "--- Block ")
		if blkStart < 0 {
			break
		}
		pymContent = pymContent[blkStart+len("--- Block "):]
		blockIdx++
		if blockIdx == 4 {
			// 找到Block 4
			blkEnd := strings.Index(pymContent, "--- Block ")
			if blkEnd < 0 {
				blkEnd = len(pymContent)
			}
			blockContent := pymContent[:blkEnd]
			// 提取Text部分
			if textIdx := strings.Index(blockContent, "Text:"); textIdx >= 0 {
				text := blockContent[textIdx+5:]
				// 清理换行
				text = strings.ReplaceAll(text, "\n", "")
				text = strings.ReplaceAll(text, "\r", "")
				fmt.Println(text[:200] + "...")
			}
			break
		}
	}
}
