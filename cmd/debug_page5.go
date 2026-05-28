package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	data, _ := os.ReadFile("output/textboxes_raw.txt")

	pageMarker := "======= PAGE 5 TextBoxes"
	idx := strings.Index(string(data), pageMarker)
	if idx < 0 {
		fmt.Println("Page 5 not found")
		return
	}

	pageEnd := "======= PAGE 6 TextBoxes"
	idxEnd := strings.Index(string(data)[idx:], pageEnd)
	content := string(data)[idx : idx+idxEnd]

	fmt.Println("=== Page 5 右侧栏分析 (X > 250) ===")
	fmt.Println()

	lines := strings.Split(content, "\n")
	tbIdx := -1

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TB[") {
			tbIdx++
			var idx int
			var Y0, Y1, X0, X1 float64
			var nLines int
			n, _ := fmt.Sscanf(line, "TB[%d] Y0=%f Y1=%f X0=%f X1=%f Lines=%d", &idx, &Y0, &Y1, &X0, &X1, &nLines)
			if n == 7 && X0 > 250 {
				fmt.Printf("TB[%2d] X0=%7.1f X1=%7.1f Y0=%7.1f Y1=%7.1f\n", 
					tbIdx, X0, X1, Y0, Y1)
			}
		}
	}
}
