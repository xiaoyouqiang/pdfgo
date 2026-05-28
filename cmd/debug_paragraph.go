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

	fmt.Println("=== Page 5 所有 TextBox Y 分布 ===")
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
			if n == 7 {
				gap := Y0 - Y1
				fmt.Printf("TB[%2d] Y0=%7.1f Y1=%7.1f gap=%6.1f X0=%6.1f X1=%6.1f\n", 
					tbIdx, Y0, Y1, gap, X0, X1)
			}
		}
	}

	fmt.Println()
	fmt.Println("=== 关键跳跃分析 ===")
	fmt.Println("Y0相邻差值超过50的点:")
	
	lines = strings.Split(content, "\n")
	var prevY0 float64 = -1
	tbIdx = -1
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TB[") {
			tbIdx++
			var idx int
			var Y0 float64
			fmt.Sscanf(line, "TB[%d] Y0=%f", &idx, &Y0)
			if prevY0 > 0 {
				diff := prevY0 - Y0
				if diff > 50 {
					fmt.Printf("  TB[%d] → TB[%d]: Y0从%.1f降到%.1f, 差值=%.1f\n", 
						tbIdx-1, tbIdx, prevY0, Y0, diff)
				}
			}
			prevY0 = Y0
		}
	}
}
