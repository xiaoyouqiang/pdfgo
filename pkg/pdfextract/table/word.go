package table

import (
	"math"
	"sort"
	"strings"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// fragment 是位置相邻的字符组成的连续片段。
//
// 设计目的：
//   原始 assignText 用字符中心点判断归属，当字符 bbox 略微越过单元格边界时
//   会被丢弃（典型场景：英文单词排到行尾时尾部字符越界几个单位）。
//   把字符聚合成 fragment 后，按整个 fragment 与 cell 的重叠面积判断归属，
//   只要 fragment 的主体落在 cell 内就会被正确分配，避免尾部丢失。
type fragment struct {
	chars []model.Char
	bbox  model.Rect
}

// OverlapArea 导出的矩形重叠面积计算接口。
func OverlapArea(a, b model.Rect) float64 {
	return overlapArea(a, b)
}
// 用于在构建 TextBoxes 之前剔除表格内字符，避免重复输出。
//
// 判断规则：用 fragment 视图，每个 fragment 只要其 bbox 与任意 cell bbox
// （向四周扩展 tolerance 单位）有非零重叠面积，则该 fragment 全部字符都被视为
// 表格内容而排除。
//
// 相比按字符中心点判断，fragment 视图能正确处理"字符越界但语义属表格"的情况：
// 比如英文单词排到行尾时尾部字符的 bbox 完全越过 cell 边界，但因为与前面的字符
// 属同一 fragment（位置相邻），而前面字符的 bbox 与 cell 重叠，整个 fragment
// 都会被排除，避免出现"ity"、"ing"等英文词尾变成孤儿 TextBox。
//
// 参数：
//   - chars: 待过滤的字符切片（不会被修改）
//   - tables: 已检测到的表格列表
//   - tolerance: cell bbox 向外扩展的容差（PDF 坐标单位）
//
// 返回：被表格消费后剩余的字符切片。
func ExcludeTableCharsFromText(chars []model.Char, tables []model.Table, tolerance float64) []model.Char {
	if len(tables) == 0 || len(chars) == 0 {
		return chars
	}

	// 收集所有非空 cell 的 bbox（扩展后）
	var expandedCells []model.Rect
	for _, t := range tables {
		for r := 0; r < t.Rows; r++ {
			for c := 0; c < t.Cols; c++ {
				cb := t.Cells[r][c].BBox
				if cb.Empty() {
					continue
				}
				// 仅 X 方向扩展（与 assignText 保持一致）：
				// Y 方向不扩展保证表格外的标题/段落（Y 与 cell 不重叠）
				// 不会被误判为表格内容。
				expandedCells = append(expandedCells, model.Rect{
					X0: cb.X0 - tolerance,
					Y0: cb.Y0,
					X1: cb.X1 + tolerance,
					Y1: cb.Y1,
				})
			}
		}
	}
	if len(expandedCells) == 0 {
		return chars
	}

	// 聚合成 fragment
	frags := groupCharsToFragments(chars)

	// 标记被消费 fragment 的所有 char SeqNo
	// 归属判断与 assignText 完全一致：只要 fragment 与任意 cell（X 扩展后）
	// 有非零重叠面积就视为表格内容；Y 方向严格不扩展，表格外文字自然不会命中。
	consumed := make(map[int]bool, len(frags))
	for _, f := range frags {
		excluded := false
		for _, ec := range expandedCells {
			if overlapArea(ec, f.bbox) > 0 {
				excluded = true
				break
			}
		}
		if excluded {
			for _, c := range f.chars {
				consumed[c.SeqNo] = true
			}
		}
	}

	var filtered []model.Char
	for _, ch := range chars {
		if !consumed[ch.SeqNo] {
			filtered = append(filtered, ch)
		}
	}
	return filtered
}

func (f fragment) text() string {
	if len(f.chars) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, c := range f.chars {
		sb.WriteString(c.Text)
	}
	return sb.String()
}

// charsBBox 计算一组字符的总边界框。
func charsBBox(chars []model.Char) model.Rect {
	if len(chars) == 0 {
		return model.Rect{}
	}
	r := chars[0].BBox
	for _, c := range chars[1:] {
		if c.BBox.X0 < r.X0 {
			r.X0 = c.BBox.X0
		}
		if c.BBox.Y0 < r.Y0 {
			r.Y0 = c.BBox.Y0
		}
		if c.BBox.X1 > r.X1 {
			r.X1 = c.BBox.X1
		}
		if c.BBox.Y1 > r.Y1 {
			r.Y1 = c.BBox.Y1
		}
	}
	return r
}

// groupCharsToFragments 按位置相邻性把字符聚合成 fragment。
//
// 聚合规则（同时满足才归入同一 fragment）：
//   1. 基线 Y 接近：用 Origin.Y（基线，比 bbox 中心稳定，不受升降序字符影响）
//      |Origin.Y 差| ≤ max(字符高) * 0.5（同一行）
//   2. 水平间距 ≤ 阈值：阈值取 max(行高, 3.0)
//      普通空格宽度通常 0.3~0.5 字号，不会触发断开；
//      只有列间空白、明显间隙才断开。
//
// 排序：按 (Origin.Y, X0) 升序。
//   注意不能用 bbox 中心 Y：'y'/'g' 等降序字符的 bbox 中心比 'h'/'b' 等升序字符
//   低几个单位，会被错误分到不同行。
// 输入切片不会被修改（内部 copy）。
func groupCharsToFragments(chars []model.Char) []fragment {
	if len(chars) == 0 {
		return nil
	}
	sorted := make([]model.Char, len(chars))
	copy(sorted, chars)
	sort.SliceStable(sorted, func(i, j int) bool {
		yi := sorted[i].Origin.Y
		yj := sorted[j].Origin.Y
		if math.Abs(yi-yj) > 2 {
			return yi < yj
		}
		return sorted[i].BBox.X0 < sorted[j].BBox.X0
	})

	var frags []fragment
	var current []model.Char
	flush := func() {
		if len(current) == 0 {
			return
		}
		// fragment 内字符按 SeqNo 排序恢复 PDF 绘制顺序。
		// 必要性：部分 PDF 的内容流绘制顺序与视觉 X 顺序不一致
		// （如 "Supplier" 的空格被绘制在 'r' 之后但 X 坐标在 'e' 和 'r' 之间）。
		// 按 X 排序会得到 "Supplie r"，按 SeqNo 才能得到 "Supplier "。
		sort.SliceStable(current, func(i, j int) bool {
			return current[i].SeqNo < current[j].SeqNo
		})
		frags = append(frags, fragment{
			chars: current,
			bbox:  charsBBox(current),
		})
		current = nil
	}

	for _, c := range sorted {
		if len(current) == 0 {
			current = []model.Char{c}
			continue
		}
		last := current[len(current)-1]

		// 规则 1：基线 Y 必须接近（同一行），用 Origin.Y 而非 bbox 中心
		if math.Abs(last.Origin.Y-c.Origin.Y) > 2 {
			flush()
			current = []model.Char{c}
			continue
		}

		// 规则 2：水平间距超过阈值则断开
		gap := c.BBox.X0 - last.BBox.X1
		lineH := last.BBox.Y1 - last.BBox.Y0
		if ch := c.BBox.Y1 - c.BBox.Y0; ch > lineH {
			lineH = ch
		}
		threshold := lineH // 1.0 * 行高
		if threshold < 3 {
			threshold = 3
		}
		if gap > threshold {
			flush()
			current = []model.Char{c}
			continue
		}

		current = append(current, c)
	}
	flush()
	return frags
}

// overlapArea 计算两个矩形的交集面积。无交集返回 0。
func overlapArea(a, b model.Rect) float64 {
	x0 := a.X0
	if b.X0 > x0 {
		x0 = b.X0
	}
	y0 := a.Y0
	if b.Y0 > y0 {
		y0 = b.Y0
	}
	x1 := a.X1
	if b.X1 < x1 {
		x1 = b.X1
	}
	y1 := a.Y1
	if b.Y1 < y1 {
		y1 = b.Y1
	}
	if x1 <= x0 || y1 <= y0 {
		return 0
	}
	return (x1 - x0) * (y1 - y0)
}
