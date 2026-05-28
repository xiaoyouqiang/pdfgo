package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	data, _ := os.ReadFile("output/textboxes_raw.txt")

	pageMarker := "======= PAGE 1 TextBoxes"
	idx := strings.Index(string(data), pageMarker)
	if idx < 0 {
		fmt.Println("Page 1 not found")
		return
	}

	pageEnd := "======= PAGE 2 TextBoxes"
	idxEnd := strings.Index(string(data)[idx:], pageEnd)
	content := string(data)[idx : idx+idxEnd]

	fmt.Println("=== Page 1 TextBox 分析 ===")
	fmt.Println()

	lines := strings.Split(content, "\n")
	tbIdx := -1

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TB[") {
			tbIdx++
			if tbIdx >= 2 && tbIdx <= 8 {
				fmt.Println(line)
			}
		} else if tbIdx >= 2 && tbIdx <= 8 && len(line) > 0 && line != "Text:" {
			fmt.Println("  " + line)
		}
	}
}
