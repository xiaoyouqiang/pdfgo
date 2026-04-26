package table

import (
	"math"
	"sort"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// BuildCells 从交叉点构建表格单元格。
// 单元格由四个交叉点围成，需要验证四个方向都有边连接。
//
// 算法：
//  1. 构建交叉点查找表（量化坐标 → 索引）
//  2. 对每个交叉点，寻找其下方和右方的交叉点
//  3. 验证四个交叉点之间都有边连接（上下左右各一条）
//  4. 四个交叉点围成的矩形区域即为一个单元格
func BuildCells(intersections []Intersection) []model.Cell {
	if len(intersections) < 4 {
		return nil
	}

	// 使用量化坐标构建查找表
	const scale = 10.0
	type key struct{ x, y int }

	lookup := make(map[key]int)
	for i, isx := range intersections {
		k := key{int(math.Round(isx.Point.X * scale)), int(math.Round(isx.Point.Y * scale))}
		lookup[k] = i
	}

	var cells []model.Cell
	n := len(intersections)

	for i := 0; i < n; i++ {
		pt := intersections[i].Point

		// 查找正下方（同 X）的交叉点
		var below []int
		for j := i + 1; j < n; j++ {
			if math.Abs(intersections[j].Point.X-pt.X) < 3.0 && intersections[j].Point.Y > pt.Y {
				below = append(below, j)
			}
		}

		// 查找正右方（同 Y）的交叉点
		var right []int
		for j := i + 1; j < n; j++ {
			if math.Abs(intersections[j].Point.Y-pt.Y) < 3.0 && intersections[j].Point.X > pt.X {
				right = append(right, j)
			}
		}

		// 尝试组合上方-右方-下方交叉点形成单元格
		for _, bi := range below {
			// 验证左边有垂直边连接
			if !edgeConnects(intersections, i, bi) {
				continue
			}
			cellFound := false
			for _, ri := range right {
				// 验证上边有水平边连接
				if !edgeConnects(intersections, i, ri) {
					continue
				}
				// 查找右下角的交叉点
				dk := key{
					int(math.Round(intersections[ri].Point.X * scale)),
					int(math.Round(intersections[bi].Point.Y * scale)),
				}
				di, ok := lookup[dk]
				if !ok {
					continue
				}
				// 验证右边和下边都有边连接
				if !edgeConnects(intersections, di, ri) {
					continue
				}
				if !edgeConnects(intersections, di, bi) {
					continue
				}
				// 四条边都验证通过，创建单元格
				cells = append(cells, model.Cell{
					BBox: model.Rect{
						X0: pt.X,
						Y0: pt.Y,
						X1: intersections[ri].Point.X,
						Y1: intersections[bi].Point.Y,
					},
				})
				cellFound = true
				break
			}
			if cellFound {
				break
			}
		}
	}
	return cells
}

// edgeConnects 检查两个交叉点之间是否有共享的边。
// 如果两个交叉点 X 坐标相同（同列），检查是否有共同的垂直边；
// 如果 Y 坐标相同（同行），检查是否有共同的水平边。
func edgeConnects(intersections []Intersection, i, j int) bool {
	pi, pj := intersections[i], intersections[j]
	// 同列（X 相同）：检查是否有共同的垂直边
	if math.Abs(pi.Point.X-pj.Point.X) < 3.0 {
		for _, vi := range pi.VEdges {
			for _, vj := range pj.VEdges {
				if vi == vj {
					return true
				}
			}
		}
	}
	// 同行（Y 相同）：检查是否有共同的水平边
	if math.Abs(pi.Point.Y-pj.Point.Y) < 3.0 {
		for _, hi := range pi.HEdges {
			for _, hj := range pj.HEdges {
				if hi == hj {
					return true
				}
			}
		}
	}
	return false
}

// GroupCells 使用迭代角点共享扩展算法将单元格分组为表格。
// 如果两个单元格共享一个角点，它们属于同一个表格。
// 算法不断扩展，直到没有新的单元格可以加入为止。
func GroupCells(cells []model.Cell) []model.Table {
	if len(cells) == 0 {
		return nil
	}

	const scale = 10.0
	type corner struct{ x, y int }

	// 计算每个单元格的四个角点
	cellCorners := make([][]corner, len(cells))
	for i, c := range cells {
		cellCorners[i] = []corner{
			{int(math.Round(c.BBox.X0 * scale)), int(math.Round(c.BBox.Y0 * scale))},
			{int(math.Round(c.BBox.X0 * scale)), int(math.Round(c.BBox.Y1 * scale))},
			{int(math.Round(c.BBox.X1 * scale)), int(math.Round(c.BBox.Y0 * scale))},
			{int(math.Round(c.BBox.X1 * scale)), int(math.Round(c.BBox.Y1 * scale))},
		}
	}

	// 跟踪哪些单元格尚未被分组
	remaining := make([]bool, len(cells))
	for i := range remaining {
		remaining[i] = true
	}

	var tables []model.Table
	for {
		// 找到第一个未分组的单元格作为起点
		start := -1
		for i, ok := range remaining {
			if ok {
				start = i
				break
			}
		}
		if start < 0 {
			break
		}

		// 初始化当前表格的角点集合
		currentCorners := make(map[corner]bool)
		var currentCells []int

		for _, c := range cellCorners[start] {
			currentCorners[c] = true
		}
		currentCells = append(currentCells, start)
		remaining[start] = false

		// 迭代扩展：将共享角点的单元格加入当前表格
		for {
			added := 0
			for i, ok := range remaining {
				if !ok {
					continue
				}
				// 检查是否有共享的角点
				shares := false
				for _, c := range cellCorners[i] {
					if currentCorners[c] {
						shares = true
						break
					}
				}
				if shares {
					currentCells = append(currentCells, i)
					for _, c := range cellCorners[i] {
						currentCorners[c] = true
					}
					remaining[i] = false
					added++
				}
			}
			if added == 0 {
				break // 没有新的单元格可加入
			}
		}

		// 至少需要 2 个单元格才能构成表格
		if len(currentCells) > 1 {
			tables = append(tables, buildTableFromCells(cells, currentCells))
		}
	}

	return tables
}

// buildTableFromCells 从单元格列表构建表格结构。
// 收集所有单元格的边界坐标，构建规则网格，并将单元格放入对应位置。
func buildTableFromCells(cells []model.Cell, indices []int) model.Table {
	// 收集所有不重复的 X 和 Y 坐标
	xSet := make(map[float64]bool)
	ySet := make(map[float64]bool)
	for _, idx := range indices {
		xSet[cells[idx].BBox.X0] = true
		xSet[cells[idx].BBox.X1] = true
		ySet[cells[idx].BBox.Y0] = true
		ySet[cells[idx].BBox.Y1] = true
	}

	// 排序得到列和行的边界
	xs := sortedKeys(xSet)
	ys := sortedKeys(ySet)

	// 构建坐标到索引的映射
	xIndex := make(map[float64]int)
	for i, x := range xs {
		xIndex[x] = i
	}
	yIndex := make(map[float64]int)
	for i, y := range ys {
		yIndex[y] = i
	}

	rows := len(ys) - 1
	cols := len(xs) - 1
	if rows <= 0 || cols <= 0 {
		return model.Table{}
	}

	// 创建规则网格并填入单元格
	grid := make([][]model.Cell, rows)
	for r := range grid {
		grid[r] = make([]model.Cell, cols)
		for c := range grid[r] {
			grid[r][c] = model.Cell{Row: r, Col: c}
		}
	}

	// 将每个单元格放入网格的正确位置
	var bbox model.Rect
	first := true
	for _, idx := range indices {
		c := cells[idx]
		r := yIndex[c.BBox.Y0]
		col := xIndex[c.BBox.X0]
		if r >= 0 && r < rows && col >= 0 && col < cols {
			grid[r][col] = model.Cell{
				BBox: c.BBox,
				Row:  r,
				Col:  col,
			}
		}
		// 计算表格的整体边界框
		if first {
			bbox = c.BBox
			first = false
		} else {
			bbox.X0 = math.Min(bbox.X0, c.BBox.X0)
			bbox.Y0 = math.Min(bbox.Y0, c.BBox.Y0)
			bbox.X1 = math.Max(bbox.X1, c.BBox.X1)
			bbox.Y1 = math.Max(bbox.Y1, c.BBox.Y1)
		}
	}
	return model.Table{
		BBox:  bbox,
		Cells: grid,
		Rows:  rows,
		Cols:  cols,
	}
}

// sortedKeys 返回 map 中所有键的排序切片
func sortedKeys(m map[float64]bool) []float64 {
	keys := make([]float64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Float64s(keys)
	return keys
}
