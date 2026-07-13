package main

import (
	"fmt"
	"os"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

func main() {
	e := pdfextract.NewExtractor(pdfextract.ExtractionOptions{
		ExtractText:  true,
		ExtractTable: true,
	})
	result, err := e.ExtractFile(os.Args[1])
	if err != nil {
		fmt.Println("err:", err)
		return
	}

	// 找到相关方表所在的页
	for _, p := range result.Pages {
		for _, t := range p.Tables {
			for _, row := range t.Cells {
				for _, cell := range row {
					if contains(cell.Text, "Customer") || contains(cell.Text, "顾客") {
						fmt.Printf("=== Page %d ===\n", p.PageNum)
						dumpOrphanChars(p, t)
						return
					}
				}
			}
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// dumpOrphanChars 找出页内所有 char，标注哪些落在表格外
func dumpOrphanChars(p model.Page, t model.Table) {
	fmt.Printf("Table bbox: X0=%.2f X1=%.2f\n", t.BBox.X0, t.BBox.X1)
	fmt.Printf("Rows=%d Cols=%d\n", t.Rows, t.Cols)
	fmt.Println()

	// 收集所有 cell 的 bbox
	cellBoxes := []struct {
		r, c int
		b    model.Rect
	}{}
	for r, row := range t.Cells {
		for c, cell := range row {
			if !cell.BBox.Empty() {
				cellBoxes = append(cellBoxes, struct {
					r, c int
					b    model.Rect
				}{r, c, cell.BBox})
			}
		}
	}

	// 关心行 1,3,5,6,8 中那些残缺的英文 cell，看越界的字符
	// 找页内所有 char
	var allChars []model.Char
	for _, tb := range p.TextBoxes {
		for _, l := range tb.Lines {
			allChars = append(allChars, l.Chars...)
		}
	}
	// 表内字符也加上（虽然表内 char 不会在 TextBox 里，但保险）
	fmt.Printf("TextBox chars count: %d\n", len(allChars))

	// 找出所有在表格 Y 范围内 但 X > 表格右边界 的孤儿字符
	fmt.Println()
	fmt.Println("=== Orphan chars (X center > cell X1 but Y in row range) ===")
	for _, ch := range allChars {
		mx := (ch.BBox.X0 + ch.BBox.X1) / 2
		my := (ch.BBox.Y0 + ch.BBox.Y1) / 2

		// 先看这个字符在表格 Y 范围内吗
		if my < t.BBox.Y0 || my > t.BBox.Y1 {
			continue
		}
		// 看落在哪一行
		rowIdx := -1
		for r := 0; r < t.Rows; r++ {
			cellY0 := t.Cells[r][0].BBox.Y0
			cellY1 := t.Cells[r][0].BBox.Y1
			// 检查这一行任意非空 cell
			for _, cc := range t.Cells[r] {
				if cc.BBox.Empty() {
					continue
				}
				cellY0 = cc.BBox.Y0
				cellY1 = cc.BBox.Y1
				break
			}
			if my >= cellY0 && my <= cellY1 {
				rowIdx = r
				break
			}
		}
		if rowIdx < 0 {
			continue
		}
		// 检查它是否落在某 cell 内
		inside := false
		for _, cb := range cellBoxes {
			if cb.r != rowIdx {
				continue
			}
			if mx >= cb.b.X0 && mx <= cb.b.X1 && my >= cb.b.Y0 && my <= cb.b.Y1 {
				inside = true
				break
			}
		}
		if !inside && ch.Text != " " {
			fmt.Printf("  orphan %q bbox=(%.2f,%.2f,%.2f,%.2f) center=(%.2f,%.2f) row=%d\n",
				ch.Text, ch.BBox.X0, ch.BBox.Y0, ch.BBox.X1, ch.BBox.Y1, mx, my, rowIdx)
		}
	}

	// 重点 dump 残缺那几行的右边界 cell
	fmt.Println()
	fmt.Println("=== Right-edge cells with truncated text ===")
	targetRows := []int{1, 3, 5, 6, 8}
	for _, r := range targetRows {
		if r >= t.Rows {
			continue
		}
		// 找最右的非空 cell
		rightCell := model.Cell{}
		rightCol := -1
		for c := t.Cols - 1; c >= 0; c-- {
			if !t.Cells[r][c].BBox.Empty() {
				rightCell = t.Cells[r][c]
				rightCol = c
				break
			}
		}
		if rightCol < 0 {
			continue
		}
		fmt.Printf("Row %d col %d: bbox=(%.2f,%.2f,%.2f,%.2f) X1=%.2f\n",
			r, rightCol, rightCell.BBox.X0, rightCell.BBox.Y0, rightCell.BBox.X1, rightCell.BBox.Y1, rightCell.BBox.X1)
		fmt.Printf("  text: %q\n", rightCell.Text)
		// 找这一行 Y 范围内、X 靠近右边界的所有 char
		var rowChars []model.Char
		for _, ch := range allChars {
			my := (ch.BBox.Y0 + ch.BBox.Y1) / 2
			if my >= rightCell.BBox.Y0 && my <= rightCell.BBox.Y1 {
				rowChars = append(rowChars, ch)
			}
		}
		// 按 X 排序打印
		// 只打印 X0 > rightCell.BBox.X1 - 30 的
		fmt.Printf("  chars with X0 > X1-30 (last 30 units before edge):\n")
		for _, ch := range rowChars {
			if ch.BBox.X0 >= rightCell.BBox.X1-30 {
				fmt.Printf("    %q bbox=(%.2f,%.2f,%.2f,%.2f) center_x=%.2f (cell.X1=%.2f, beyond=%v)\n",
					ch.Text, ch.BBox.X0, ch.BBox.Y0, ch.BBox.X1, ch.BBox.Y1,
					(ch.BBox.X0+ch.BBox.X1)/2, rightCell.BBox.X1,
					(ch.BBox.X0+ch.BBox.X1)/2 > rightCell.BBox.X1)
			}
		}
		fmt.Println()
	}
}
