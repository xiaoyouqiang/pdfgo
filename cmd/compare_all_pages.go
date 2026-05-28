package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	data, _ := os.ReadFile("output/textboxes_raw.txt")
	content := string(data)

	lines := strings.Split(content, "\n")

	page := 0
	tbCount := 0
	fmt.Printf("Page  Block Count\n")
	fmt.Printf("----  -----------\n")

	for _, line := range lines {
		if strings.Contains(line, "======= PAGE") {
			if page > 0 {
				fmt.Printf("%3d    %d\n", page-1, tbCount)
			}
			page++
			tbCount = 0
		}
		if strings.HasPrefix(strings.TrimSpace(line), "TB[") {
			tbCount++
		}
	}
	if page > 0 {
		fmt.Printf("%3d    %d\n", page-1, tbCount)
	}
}