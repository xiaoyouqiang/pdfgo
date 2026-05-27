// Package layout 实现字符到文本框的布局分析。
// 将内容流解释器输出的原始字符按绘制顺序和空间位置关系组织为文本行和文本框，
// 采用 MuPDF 风格的笔位跟踪算法实现多栏 PDF 的正确分栏检测。
package layout

import (
	"math"
	"sort"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// MuPDF 风格笔位跟踪算法的阈值常量。
// 这些阈值将位置偏移按字号归一化后进行判断，
// 使得算法对不同字号的文档具有良好的适应性。
// 来源：MuPDF stext-device.c 中的 fz_add_stext_char_imp()
const (
	// paragraphDist 控制何时创建新的文本块。
	// 当垂直偏移超过 fontSize * paragraphDist 时，开始新的文本块。
	paragraphDist = 1.5

	// baseMaxDist 控制字符是否在同一基线上。
	// 当垂直偏移小于 fontSize * baseMaxDist 时，认为在同一行。
	baseMaxDist = 0.8

	// spaceMaxDist 控制同行字符的水平间距阈值。
	// 当水平间距超过 fontSize * spaceMaxDist 时，认为可能是新的列。
	spaceMaxDist = 0.8
)

// Params 控制布局分析的行为参数。
type Params struct {
	CharMargin float64 // 字符间距容差（已弃用，当前未使用）
	LineMargin float64 // 行间距容差倍数，控制相邻行是否归入同一文本框
	WordMargin float64 // 词间距容差倍数，控制字符之间何时插入空格
}

// DefaultParams 返回默认的布局分析参数
func DefaultParams() Params {
	return Params{
		CharMargin: 2.0,
		LineMargin: 0.5,
		WordMargin: 0.1,
	}
}

// Analyze 将字符分组为按阅读顺序排列的文本框。
//
// 采用 MuPDF 风格的笔位跟踪算法（pen-tracking），按照 PDF 内容流的绘制顺序
// 处理字符，而非几何排序。这确保了多栏 PDF 中各栏文本被正确分离为不同的文本块。
//
// 处理流程：
//  1. 按 SeqNo（绘制序号）排序，恢复内容流绘制顺序
//  2. penTrackGroup：笔位跟踪算法将字符逐个分组为行和块
//  3. mergeSuperscriptLines：合并上标行
//  4. sortTextBoxes：按阅读顺序排序文本框
func Analyze(chars []model.Char, params Params) []model.TextBox {
	if len(chars) == 0 {
		return nil
	}

	// 按 SeqNo 排序，恢复内容流绘制顺序。
	// 这是与旧算法的核心区别：旧算法按 (Y desc, X asc) 排序，
	// 导致左右栏字符交错；新算法按绘制顺序处理，天然保持分栏。
	sorted := make([]model.Char, len(chars))
	copy(sorted, chars)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].SeqNo < sorted[j].SeqNo
	})

	// 笔位跟踪：按绘制顺序将字符分组为行和块
	blocks := penTrackGroup(sorted, params)

	// 后处理：合并上标行
	for i := range blocks {
		blocks[i].Lines = mergeSuperscriptLines(blocks[i].Lines)
		if len(blocks[i].Lines) > 0 {
			blocks[i].BBox = computeBoxBBox(blocks[i].Lines)
		}
	}

	// 后处理：拆分跨越栏边界的块
	// 笔位跟踪可能将同一基线上但属于不同栏的行归入同一块
	// （例如 "Abstract" 标签和右栏文本在同一 Y 位置）。
	// 通过检测块内连续行是否无 X 重叠来拆分。
	blocks = splitBlocksAtColumnGaps(blocks)

	// 按阅读顺序排序
	sortTextBoxes(blocks)

	return blocks
}

// penTrackGroup 使用笔位跟踪算法将字符分组为文本框。
//
// 算法原理（源自 MuPDF 的 stext-device.c 中的 fz_add_stext_char_imp）：
//
//	维护一个"笔位"（pen position），记录上一个字符结束的位置。
//	对于每个新字符，计算其相对于笔位的偏移量，按字号归一化后判断：
//	  - |baseOffset| < 0.8：同一基线 → 同一行
//	  - 0.8 ≤ |baseOffset| ≤ 1.5：基线偏移适中 → 新行（同块）
//	  - |baseOffset| > 1.5：基线偏移过大 → 新块（新段落/新栏）
//
// 多栏检测原理：
//
//	PDF 内容流通常先画完左栏（从上到下），再画右栏（从上到下）。
//	当画完左栏底部跳到右栏顶部时，Y 方向产生大幅跳变（baseOffset >> 1.5），
//	算法自动创建新块，无需显式的栏检测。
func penTrackGroup(chars []model.Char, params Params) []model.TextBox {
	var blocks []model.TextBox
	var curLines []model.TextLine
	var curLineChars []model.Char

	// 笔位状态
	penX := 0.0
	penY := 0.0
	penFontSize := 12.0
	initialized := false

	for _, ch := range chars {
		fs := ch.Font.Size
		if fs <= 0 {
			fs = penFontSize
		}
		if fs <= 0 {
			fs = 12
		}

		if !initialized {
			curLineChars = []model.Char{ch}
			penX = ch.Origin.X + ch.Advance
			penY = ch.Origin.Y
			penFontSize = fs
			initialized = true
			continue
		}

		// 计算字符起始位置相对于笔位的偏移
		dx := ch.Origin.X - penX
		dy := ch.Origin.Y - penY

		// 按字号归一化
		spacing := dx / fs    // 沿基线方向的位移（正=向右）
		baseOffset := dy / fs // 垂直于基线的位移（正=向上）
		absBase := math.Abs(baseOffset)

		newPara := false
		newLine := false

	// 检测上标字符：字号明显小于当前笔位字号且位于笔位下方（Y 增大的方向）
	// 上标字符的 origin.Y 会比主文本基线略大（页面坐标系 Y 向下）
	isSuper := fs < penFontSize*0.75 && baseOffset > 0

	if absBase < baseMaxDist {
		// 在同一基线上（或非常接近）
		if math.Abs(spacing) >= spaceMaxDist && !isSuper {
			// 水平间距过大，可能是表格列 → 新行
			// 但上标字符即使水平间距大，也应保持同行（因为它本来就该在主文本右上方）
			newLine = true
		}
		// 否则：同一行，不中断
		} else if absBase <= paragraphDist {
			if isSuper {
				// 上标字符 → 保持在同一行
			} else {
				// 基线偏移适中 → 新行，同块
				newLine = true
			}
		} else {
			if isSuper {
				// 上标字符（即使 Y 偏移很大）→ 保持在同一行
			} else {
				// 基线偏移过大 → 新块
				newPara = true
				newLine = true
			}
		}

		if newPara {
			// 结束当前行
			if len(curLineChars) > 0 {
				curLines = append(curLines, finalizeLine(curLineChars, params))
				curLineChars = nil
			}
			// 结束当前块
			if len(curLines) > 0 {
				blocks = append(blocks, model.TextBox{
					Lines: curLines,
					BBox:  computeBoxBBox(curLines),
				})
				curLines = nil
			}
		} else if newLine {
			// 结束当前行
			if len(curLineChars) > 0 {
				curLines = append(curLines, finalizeLine(curLineChars, params))
				curLineChars = nil
			}
		}

		curLineChars = append(curLineChars, ch)

		// 更新笔位
		penX = ch.Origin.X + ch.Advance
		penY = ch.Origin.Y
		penFontSize = fs
	}

	// 处理剩余的行和块
	if len(curLineChars) > 0 {
		curLines = append(curLines, finalizeLine(curLineChars, params))
	}
	if len(curLines) > 0 {
		blocks = append(blocks, model.TextBox{
			Lines: curLines,
			BBox:  computeBoxBBox(curLines),
		})
	}

	return blocks
}

// splitBlocksAtColumnGaps 检查每个块内的连续行，如果相邻行无 X 方向重叠，
// 说明它们属于不同的栏，应拆分为独立的块。
//
// 解决的问题：笔位跟踪按 Y 偏移判断行/块边界，当两个文本段位于同一基线
// 但分属左右栏时（如 "Abstract" 标签和右栏文本），会被错误归入同一块。
// 通过 X 重叠检测可以发现并修正这种错误。
func splitBlocksAtColumnGaps(blocks []model.TextBox) []model.TextBox {
	var result []model.TextBox
	for _, block := range blocks {
		result = append(result, splitBlockAtColumnGap(block)...)
	}
	return result
}

// splitBlockAtColumnGap 将单个块按栏间隙拆分。
// 遍历块内连续行，如果当前行与上一行无 X 重叠，则在此处拆分。
func splitBlockAtColumnGap(block model.TextBox) []model.TextBox {
	if len(block.Lines) <= 1 {
		return []model.TextBox{block}
	}

	var groups [][]model.TextLine
	var current []model.TextLine

	for _, line := range block.Lines {
		if len(current) > 0 {
			prevLine := current[len(current)-1]
			// 检查 X 方向是否有重叠
			overlapStart := math.Max(prevLine.BBox.X0, line.BBox.X0)
			overlapEnd := math.Min(prevLine.BBox.X1, line.BBox.X1)
			hasXOverlap := overlapEnd > overlapStart

			if !hasXOverlap {
				// 无 X 重叠，检查是否可能是上标行（不应拆分）
				if isSuperscriptLinePair(prevLine, line) {
					// 当前行是上标行，保持在同一块中
				} else {
					// 无 X 重叠且非上标 → 不同栏，拆分
					groups = append(groups, current)
					current = nil
				}
			}
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}

	if len(groups) <= 1 {
		return []model.TextBox{block}
	}

	var result []model.TextBox
	for _, lines := range groups {
		tb := model.TextBox{Lines: lines}
		if len(lines) > 0 {
			tb.BBox = computeBoxBBox(lines)
		}
		result = append(result, tb)
	}
	return result
}

// --- 以下为保留的辅助函数 ---

// finalizeLine 完成一行文本的构建：排序字符，插入词间空格，计算边界框。
// 当检测到同字体X重叠时（双层渲染PDF），按绘制序号排序以保持内容流顺序；
// 否则按X坐标排序（普通PDF的正常布局顺序）。
func finalizeLine(chars []model.Char, params Params) model.TextLine {
	if hasSameFontOverlap(chars) {
		sort.Slice(chars, func(i, j int) bool {
			return chars[i].SeqNo < chars[j].SeqNo
		})
	} else {
		sort.Slice(chars, func(i, j int) bool {
			return chars[i].Origin.X < chars[j].Origin.X
		})
	}

	// 估算平均字符前进宽度
	avgAdvance := 0.0
	count := 0
	for _, c := range chars {
		if c.Advance > 0 {
			avgAdvance += c.Advance
			count++
		}
	}
	if count > 0 {
		avgAdvance /= float64(count)
	}

	// 在字符间隙过大处插入空格
	var result []model.Char
	for i, ch := range chars {
		if i > 0 && len(result) > 0 && avgAdvance > 0 {
			prev := result[len(result)-1]
			gap := ch.Origin.X - (prev.Origin.X + prev.Advance)
			// 间隙超过平均宽度的一定倍数时插入空格
			if gap > avgAdvance*params.WordMargin*10 {
				spaceOrigin := model.Point{X: prev.Origin.X + prev.Advance, Y: prev.Origin.Y}
				fs := math.Abs(prev.Font.Size)
				result = append(result, model.Char{
					Text:    " ",
					Origin:  spaceOrigin,
					Advance: gap,
					Font:    prev.Font,
					BBox: model.Rect{
						X0: spaceOrigin.X,
						Y0: spaceOrigin.Y - fs*0.8,
						X1: spaceOrigin.X + gap,
						Y1: spaceOrigin.Y + fs*0.8,
					},
				})
			}
		}
		result = append(result, ch)
	}

	var bbox model.Rect
	if len(result) > 0 {
		bbox = lineBBox(result)
	}

	return model.TextLine{Chars: result, BBox: bbox}
}

// mergeSuperscriptLines 基于 SeqNo（内容流绘制顺序）逐字符合并上标行。
//
// 采用两遍处理：
//
//	第一遍：收集所有非上标行到 result（确保所有主文本行就绪）
//	第二遍：对每个上标字符按 SeqNo 找到前驱字符，插入到其之后
//
// 必须两遍处理的原因：PDF 内容流中上标与作者名是交错绘制的：
//
//	[(Zhiyong Wu )]TJ    → SeqNo 124-133（第二行作者）
//	4.338 Td              → 移到上标位置
//	[(1,2,†)]TJ           → SeqNo 134-138（上标，属于第二行作者）
//	-4.338 Td             → 返回
//	[(, Yiwen Guo )]TJ    → SeqNo 139-149
//	...
func mergeSuperscriptLines(lines []model.TextLine) []model.TextLine {
	if len(lines) < 2 {
		return lines
	}

	// 第一遍：分离上标行和主文本行
	var result []model.TextLine
	var superscriptLines []model.TextLine
	for _, line := range lines {
		if isSuperscriptBySize(line) {
			superscriptLines = append(superscriptLines, line)
		} else {
			result = append(result, line)
		}
	}

	if len(superscriptLines) == 0 {
		return result
	}

	// 第二遍：按 SeqNo 逐字符合并上标到主文本行
	for _, supLine := range superscriptLines {
		supChars := make([]model.Char, len(supLine.Chars))
		copy(supChars, supLine.Chars)
		sort.Slice(supChars, func(a, b int) bool {
			return supChars[a].SeqNo < supChars[b].SeqNo
		})

		for _, sc := range supChars {
			lineIdx, charIdx := findInsertPosition(result, sc.SeqNo)
			if lineIdx >= 0 && charIdx >= 0 {
				target := &result[lineIdx]
				insertAt := charIdx + 1
				target.Chars = append(
					target.Chars[:insertAt],
					append([]model.Char{sc}, target.Chars[insertAt:]...)...,
				)
				target.BBox = lineBBox(target.Chars)
			}
		}
	}

	return result
}

// isSuperscriptBySize 仅通过字号判断是否为上标行。
func isSuperscriptBySize(line model.TextLine) bool {
	avgSize := avgFontSizeInLine(line.Chars)
	if avgSize <= 0 {
		return false
	}
	if avgSize >= 8 {
		return false
	}
	if len(line.Chars) > 30 {
		return false
	}
	return true
}

// findInsertPosition 在所有已有行中找到 SeqNo 最近的前驱字符位置。
func findInsertPosition(result []model.TextLine, targetSeqNo int) (int, int) {
	bestLineIdx := -1
	bestCharIdx := -1
	bestSeq := -1

	for li, line := range result {
		for ci, c := range line.Chars {
			if c.SeqNo < targetSeqNo && c.SeqNo > bestSeq {
				bestLineIdx = li
				bestCharIdx = ci
				bestSeq = c.SeqNo
			}
		}
	}

	return bestLineIdx, bestCharIdx
}

// avgFontSizeInLine 计算一行中所有字符的平均字号。
func avgFontSizeInLine(chars []model.Char) float64 {
	if len(chars) == 0 {
		return 0
	}
	var sum, count float64
	for _, c := range chars {
		if c.Font.Size > 0 {
			sum += c.Font.Size
			count++
		}
	}
	if count > 0 {
		return sum / count
	}
	return 0
}

// hasSameFontOverlap 检测同一字体中是否有字符在 X 方向上重叠。
func hasSameFontOverlap(chars []model.Char) bool {
	type fontSpan struct{ x0, x1 float64 }
	spans := make(map[string][]fontSpan)
	for _, c := range chars {
		x0 := c.Origin.X
		x1 := c.Origin.X + c.Advance
		s := spans[c.Font.Name]
		for _, existing := range s {
			overlap := math.Min(x1, existing.x1) - math.Max(x0, existing.x0)
			if overlap > 0 {
				return true
			}
		}
		spans[c.Font.Name] = append(spans[c.Font.Name], fontSpan{x0, x1})
	}
	return false
}

// isSuperscriptLinePair 检测相邻两行是否构成上标行对。
// 如果前一行 Y 较高（值较小）且字号明显更大，且两行 X 方向有重叠，
// 则认为后一行是前一行中上标字符分离出来形成的独立行。
func isSuperscriptLinePair(prevLine, curLine model.TextLine) bool {
	prevSize := avgFontSizeInLine(prevLine.Chars)
	curSize := avgFontSizeInLine(curLine.Chars)

	// 如果当前行字号明显更小，可能是上标行
	if curSize >= prevSize*0.8 {
		return false
	}

	// 如果两行 Y 坐标接近（差值小于小字号的一倍），可能是上标
	// 页面坐标系 Y 向下，所以 Y 值小 = 在上方
	yDiff := prevLine.BBox.Y0 - curLine.BBox.Y0
	if yDiff < 0 || yDiff > curSize*2 {
		return false
	}

	// 检查 X 方向是否有重叠（允许上标比主文本稍左或稍右）
	overlapStart := math.Max(prevLine.BBox.X0, curLine.BBox.X0)
	overlapEnd := math.Min(prevLine.BBox.X1, curLine.BBox.X1)

	// 如果两行 X 有重叠，或者 curLine 整体在 prevLine 右侧不远处，认为是上标
	// 上标通常紧跟在主文本右侧
	if overlapEnd > overlapStart {
		return true // X 有重叠
	}

	// 如果 curLine 左边界紧跟在 prevLine 右侧（间距小于平均字符宽度）
	avgWidth := (prevLine.BBox.X1 - prevLine.BBox.X0) / math.Max(float64(len(prevLine.Chars)), 1)
	if curLine.BBox.X0 > prevLine.BBox.X0 && curLine.BBox.X0 < prevLine.BBox.X1+avgWidth*1.5 {
		return true
	}

	return false
}

// lineBBox 计算一行字符的整体边界框。
func lineBBox(chars []model.Char) model.Rect {
	if len(chars) == 0 {
		return model.Rect{}
	}
	// 跳过无效边界框的字符，避免它们拉偏整行边界
	// 但通过 Origin 计算的边界仍然会包含这些字符
	first := -1
	for i, c := range chars {
		if c.BBox.X0 < c.BBox.X1 && c.BBox.Y0 < c.BBox.Y1 {
			first = i
			break
		}
	}
	if first < 0 {
		return model.Rect{}
	}
	x0, y0 := chars[first].BBox.X0, chars[first].BBox.Y0
	x1, y1 := chars[first].BBox.X1, chars[first].BBox.Y1
	for _, c := range chars[first+1:] {
		if c.BBox.X0 < c.BBox.X1 && c.BBox.Y0 < c.BBox.Y1 {
			x0 = math.Min(x0, c.BBox.X0)
			y0 = math.Min(y0, c.BBox.Y0)
			x1 = math.Max(x1, c.BBox.X1)
			y1 = math.Max(y1, c.BBox.Y1)
		}
	}
	for _, c := range chars {
		x0 = math.Min(x0, c.Origin.X)
		y0 = math.Min(y0, c.Origin.Y-math.Abs(c.Font.Size)*0.2)
		x1 = math.Max(x1, c.Origin.X+c.Advance)
		y1 = math.Max(y1, c.Origin.Y+math.Abs(c.Font.Size)*0.8)
	}
	return model.Rect{X0: x0, Y0: y0, X1: x1, Y1: y1}
}

// computeBoxBBox 计算文本框中所有行的整体边界框
func computeBoxBBox(lines []model.TextLine) model.Rect {
	if len(lines) == 0 {
		return model.Rect{}
	}
	bbox := lines[0].BBox
	for _, l := range lines[1:] {
		bbox.X0 = math.Min(bbox.X0, l.BBox.X0)
		bbox.Y0 = math.Min(bbox.Y0, l.BBox.Y0)
		bbox.X1 = math.Max(bbox.X1, l.BBox.X1)
		bbox.Y1 = math.Max(bbox.Y1, l.BBox.Y1)
	}
	return bbox
}

// sortTextBoxes 按视觉位置排序文本框：Y 值大的优先（页面上方），同高度时 X 小的优先（左侧）
func sortTextBoxes(boxes []model.TextBox) {
	sort.Slice(boxes, func(i, j int) bool {
		bi, bj := boxes[i].BBox, boxes[j].BBox
		if math.Abs(bi.Y0-bj.Y0) > 5 {
			return bi.Y0 > bj.Y0
		}
		return bi.X0 < bj.X0
	})
}
