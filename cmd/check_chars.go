package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	// 读取PyMuPDF的Page 1输出，检查Block 1的Y坐标
	pymData, _ := os.ReadFile("output/pymupdf_output.txt")
	
	idx := strings.Index(string(pymData), "\r\nPAGE 1\r\n")
	idxEnd := strings.Index(string(pymData)[idx:], "\r\n============================================================\r\nPAGE 2\r\n")
	content := string(pymData)[idx : idx+idxEnd]
	
	// Block 1是作者行
	blockStart := strings.Index(content, "--- Block 1 ---")
	if blockStart >= 0 {
		blockContent := content[blockStart:]
		blockEnd := strings.Index(blockContent, "--- Block ")
		if blockEnd > 0 {
			blockContent = blockContent[:blockEnd]
		}
		lines := strings.Split(blockContent, "\r\n")
		for _, line := range lines {
			fmt.Println(line)
		}
	}
}
