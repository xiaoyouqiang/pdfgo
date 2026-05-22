// Package table 边缘线条检测算法的 PDF 表格检测。
//
// 检测流程：
//  1. 将 PDF 矩形和线段转换为边（Edge）
//  2. 对相近的边进行捕捉（Snap）和合并（Join）
//  3. 查找边的交叉点（Intersection）
//  4. 从交叉点构建单元格（Cell）
//  5. 将相邻单元格分组为表格（Table）
//  6. 将文本字符分配到对应的单元格中
package table

import (
	"math"
	"sort"
	"strings"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// TableSettings 配置表格检测的容差参数。
type TableSettings struct {
	SnapTolerance    float64 // 捕捉容差：将相近的边聚合到平均位置
	JoinTolerance    float64 // 合并容差：合并共线且重叠的边
	EdgeMinLength    float64 // 最小边长度：忽略过短的边
	IntersectionXTol float64 // 交叉点 X 容差
	IntersectionYTol float64 // 交叉点 Y 容差
	MinCellWidth     float64 // 最小单元格宽度
	MinCellHeight    float64 // 最小单元格高度
	MinTableCells    int     // 表格最少包含的单元格数
	MinRectHeight    float64 // 矩形最小高度：低于此值的矩形视为水平分隔线而非单元格
	PageWidth        float64 // 页面宽度（用于过滤全页矩形）
	PageHeight       float64 // 页面高度（用于过滤全页矩形）
}

// DefaultSettings 返回默认的表格检测参数
func DefaultSettings() TableSettings {
	return TableSettings{
		SnapTolerance:    3.0,
		JoinTolerance:    3.0,
		EdgeMinLength:    3.0,
		IntersectionXTol: 3.0,
		IntersectionYTol: 3.0,
		MinCellWidth:     5.0,
		MinCellHeight:    5.0,
		MinTableCells:    2,
		MinRectHeight:    1.0,
	}
}

// Detect 在解释结果中检测表格。
// 过滤矩形 → 提取边 → 捕捉 → 合并 → 交叉点 → 单元格 → 分组 → 表格
func Detect(result *model.InterpretResult, settings TableSettings) []model.Table {
	// 过滤掉全页背景矩形，然后从矩形提取边
	rects := filterPageRects(result.Rects, settings)
	edges := RectsToEdges(rects)
	// 同时从线段提取边
	edges = append(edges, LinesToEdges(result.Lines)...)

	return detectFromEdges(edges, result.Chars, settings)
}

// filterPageRects 过滤掉不适合用于表格检测的矩形：
//   - 全页背景矩形（尺寸接近页面大小的 95%）
//   - 过大的矩形（面积超过页面 50%）
//   - 非常细的水平分隔线（高度 < 2）
func filterPageRects(rects []model.Rect, settings TableSettings) []model.Rect {
	pw, ph := settings.PageWidth, settings.PageHeight
	pageArea := pw * ph

	var filtered []model.Rect
	for _, r := range rects {
		if r.Empty() {
			continue
		}
		// 跳过接近页面大小的矩形（背景）
		if pw > 0 && ph > 0 {
			if r.Width() > pw*0.95 && r.Height() > ph*0.95 {
				continue
			}
		}
		// 跳过面积超过页面 50% 的矩形
		if pageArea > 0 && r.Area() > pageArea*0.5 {
			continue
		}
		// 跳过非常细的水平分隔线（高度低于阈值）
		if r.Height() < settings.MinRectHeight {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// detectFromEdges 从边集合中检测表格的核心算法。
func detectFromEdges(edges []Edge, chars []model.Char, settings TableSettings) []model.Table {
	if len(edges) == 0 {
		return nil
	}

	// 第一步：过滤过短的边
	var filtered []Edge
	for _, e := range edges {
		if e.Length() >= settings.EdgeMinLength {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	// 第二步：捕捉（将相近的平行边聚合到平均位置）
	merged := SnapEdges(filtered, settings.SnapTolerance)
	// 第三步：合并（将共线且重叠的边合并为一条长边）
	merged = JoinEdges(merged, settings.JoinTolerance)
	// 第三步半：移除装饰性水平线（如双语表头中的分隔线）
	merged = filterDecorativeHEdges(merged, 30.0, 5.0)

	// 第四步：查找所有水平边和垂直边的交叉点
	intersections := FindIntersections(merged, settings.IntersectionXTol, settings.IntersectionYTol)
	if len(intersections) < 4 {
		return nil
	}

	// 第五步：从交叉点构建单元格
	cells := BuildCells(intersections, settings.IntersectionXTol, settings.IntersectionYTol)
	if len(cells) == 0 {
		return nil
	}

			
	// 过滤过小的单元格// 过滤过小的单元格
	var filteredCells []model.Cell
	for _, c := range cells {
		if c.BBox.Width() >= settings.MinCellWidth && c.BBox.Height() >= settings.MinCellHeight {
			filteredCells = append(filteredCells, c)
		}
	}
	if len(filteredCells) == 0 {
		return nil
	}

	// 第六步：将相邻单元格分组为表格
	tables := GroupCells(filteredCells)

	// 过滤过小的表格
	var kept []model.Table
	for _, tbl := range tables {
		if tbl.Rows*tbl.Cols >= settings.MinTableCells {
			kept = append(kept, tbl)
		}
	}

	// 第七步：将文本字符分配到各单元格中
	for i := range kept {
		assignText(&kept[i], chars)
		// PDF 坐标系 Y 轴向上，需要翻转行序以匹配视觉顺序
		kept[i] = reverseRowOrder(kept[i])
	}

	// 按视觉位置排序表格
	sort.Slice(kept, func(i, j int) bool {
		dy := kept[i].BBox.Y1 - kept[j].BBox.Y1
		if math.Abs(dy) > 1 {
			return dy > 0
		}
		return kept[i].BBox.X0 < kept[j].BBox.X0
	})

	return kept
}

// assignText 将字符分配到表格的各个单元格中。
// 使用字符边界框中心点判断字符属于哪个单元格。
func assignText(tbl *model.Table, chars []model.Char) {
	for r := 0; r < tbl.Rows; r++ {
		for c := 0; c < tbl.Cols; c++ {
			cell := &tbl.Cells[r][c]
			if cell.BBox.Empty() {
				continue
			}
			var sb strings.Builder
			for _, ch := range chars {
				// 使用字符中心点判断归属
				mx := (ch.BBox.X0 + ch.BBox.X1) / 2
				my := (ch.BBox.Y0 + ch.BBox.Y1) / 2
				if cell.BBox.Contains(model.Point{X: mx, Y: my}) {
					sb.WriteString(ch.Text)
				}
			}
			cell.Text = sb.String()
		}
	}
}

// reverseRowOrder 翻转表格的行序。
// PDF 坐标系 Y 轴向上，表格检测时行序是从下到上，
// 需要翻转为从上到下以匹配视觉阅读顺序。
func reverseRowOrder(tbl model.Table) model.Table {
	if tbl.Rows <= 1 {
		return tbl
	}
	grid := make([][]model.Cell, tbl.Rows)
	for r := range grid {
		grid[r] = make([]model.Cell, tbl.Cols)
		src := tbl.Rows - 1 - r // 源行索引（倒序）
		for c := 0; c < tbl.Cols; c++ {
			grid[r][c] = tbl.Cells[src][c]
			grid[r][c].Row = r
			grid[r][c].Col = c
		}
	}
	return model.Table{
		BBox:  tbl.BBox,
		Cells: grid,
		Rows:  tbl.Rows,
		Cols:  tbl.Cols,
	}
}

// splitMergedTables 检测并拆分因共享边框而被错误合并的表格。
// 算法：将单元格按 Y 坐标分行，计算相邻行的 X 边界重叠率，
// 如果重叠率骤降，说明列结构发生跳变，在此处拆分。
func splitMergedTables(tables []model.Table) []model.Table {
	var result []model.Table
	for _, tbl := range tables {
		splits := splitByColumnDiscontinuity(tbl)
		result = append(result, splits...)
	}
	return result
}

// splitByColumnDiscontinuity 分析表格中每行的 X 边界集合，
// 在相邻行 X 边界重叠率骤降处拆分表格。
func splitByColumnDiscontinuity(tbl model.Table) []model.Table {
	if tbl.Rows <= 2 {
		return []model.Table{tbl}
	}

	// 1. 提取每行的 X 边界集合（量化后 snap 消除浮点噪声）
	const q = 10.0
	const snapTol = 5
	rowXBorders := make([]map[int]bool, tbl.Rows)
	for r := 0; r < tbl.Rows; r++ {
		rowXBorders[r] = make(map[int]bool)
		for c := 0; c < tbl.Cols; c++ {
			cell := tbl.Cells[r][c]
			if !cell.BBox.Empty() {
				x0 := int(math.Round(cell.BBox.X0 * q))
				x1 := int(math.Round(cell.BBox.X1 * q))
				rowXBorders[r][x0] = true
				rowXBorders[r][x1] = true
			}
		}
	}

	// 2. 对每行的 X 边界做 snap 合并
	for r := range rowXBorders {
		keys := sortedIntKeys(rowXBorders[r])
		snapped := snapSortedInts(keys, snapTol)
		m := make(map[int]bool, len(snapped))
		for _, k := range snapped {
			m[k] = true
		}
		rowXBorders[r] = m
	}

	// 3. 计算相邻行的 X 边界重叠率 (Jaccard)
	overlapRatios := make([]float64, tbl.Rows-1)
	for r := 0; r < tbl.Rows-1; r++ {
		setA := rowXBorders[r]
		setB := rowXBorders[r+1]
		intersection := 0
		for k := range setA {
			if setB[k] {
				intersection++
			}
		}
		union := len(setA) + len(setB) - intersection
		if union == 0 {
			overlapRatios[r] = 1.0
		} else {
			overlapRatios[r] = float64(intersection) / float64(union)
		}
	}

	// 4. 找到重叠率骤降的位置作为拆分点
	var validRatios []float64
	for _, ratio := range overlapRatios {
		validRatios = append(validRatios, ratio)
	}
	sort.Float64s(validRatios)
	medianRatio := validRatios[len(validRatios)/2]

	// 阈值: 重叠率 < 中位数的 40% 且 < 0.5
	var splitAfter []int
	for r, ratio := range overlapRatios {
		if ratio < medianRatio*0.4 && ratio < 0.5 {
			splitAfter = append(splitAfter, r)
		}
	}

	if len(splitAfter) == 0 {
		return []model.Table{tbl}
	}

	// 5. 按拆分点切割
	var result []model.Table
	prev := 0
	for _, sp := range splitAfter {
		if sp+1-prev >= 1 {
			result = append(result, extractSubTable(tbl, prev, sp+1))
		}
		prev = sp + 1
	}
	if tbl.Rows-prev >= 1 {
		result = append(result, extractSubTable(tbl, prev, tbl.Rows))
	}

	if len(result) <= 1 {
		return []model.Table{tbl}
	}
	return result
}

// extractSubTable 从表格中提取指定行范围的子表格。
func extractSubTable(tbl model.Table, startRow, endRow int) model.Table {
	rows := endRow - startRow
	grid := make([][]model.Cell, rows)
	var bbox model.Rect
	first := true
	for r := 0; r < rows; r++ {
		grid[r] = make([]model.Cell, tbl.Cols)
		for c := 0; c < tbl.Cols; c++ {
			cell := tbl.Cells[startRow+r][c]
			cell.Row = r
			cell.Col = c
			grid[r][c] = cell
			if !cell.BBox.Empty() {
				if first {
					bbox = cell.BBox
					first = false
				} else {
					bbox.X0 = math.Min(bbox.X0, cell.BBox.X0)
					bbox.Y0 = math.Min(bbox.Y0, cell.BBox.Y0)
					bbox.X1 = math.Max(bbox.X1, cell.BBox.X1)
					bbox.Y1 = math.Max(bbox.Y1, cell.BBox.Y1)
				}
			}
		}
	}
	return model.Table{
		BBox:  bbox,
		Cells: grid,
		Rows:  rows,
		Cols:  tbl.Cols,
	}
}
