package table

import (
	"math"
	"sort"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// Orientation 表示边的方向：水平或垂直
type Orientation int

const (
	Horizontal Orientation = iota // 水平边
	Vertical                      // 垂直边
)

// Edge 表示一条轴对齐的线段（水平或垂直）。
// 从 PDF 的矩形路径和线段路径中提取，是表格检测的基础元素。
type Edge struct {
	Orientation Orientation
	X0, Y0      float64 // 起点坐标
	X1, Y1      float64 // 终点坐标
}

// Length 返回边的长度
func (e Edge) Length() float64 {
	if e.Orientation == Horizontal {
		return math.Abs(e.X1 - e.X0)
	}
	return math.Abs(e.Y1 - e.Y0)
}

// RectsToEdges 将每个矩形分解为 4 条边（上、下、左、右）。
// 矩形的四条边将用于后续的表格边框检测。
func RectsToEdges(rects []model.Rect) []Edge {
	var edges []Edge
	for _, r := range rects {
		if r.Empty() {
			continue
		}
		edges = append(edges, Edge{Horizontal, r.X0, r.Y1, r.X1, r.Y1}) // 上边
		edges = append(edges, Edge{Horizontal, r.X0, r.Y0, r.X1, r.Y0}) // 下边
		edges = append(edges, Edge{Vertical, r.X0, r.Y0, r.X0, r.Y1})   // 左边
		edges = append(edges, Edge{Vertical, r.X1, r.Y0, r.X1, r.Y1})   // 右边
	}
	return edges
}

// LinesToEdges 将线段转换为边，丢弃非轴对齐的线段。
// PDF 内容流中的线段可能是任意方向的，表格检测只关心水平和垂直线段。
func LinesToEdges(lines []model.LineSegment) []Edge {
	var edges []Edge
	for _, l := range lines {
		dx := math.Abs(l.X1 - l.X0)
		dy := math.Abs(l.Y1 - l.Y0)
		if dx < 0.5 && dy > 0.5 {
			// 基本垂直的线段
			y0, y1 := l.Y0, l.Y1
			if y0 > y1 {
				y0, y1 = y1, y0
			}
			edges = append(edges, Edge{Vertical, l.X0, y0, l.X0, y1})
		} else if dy < 0.5 && dx > 0.5 {
			// 基本水平的线段
			x0, x1 := l.X0, l.X1
			if x0 > x1 {
				x0, x1 = x1, x0
			}
			edges = append(edges, Edge{Horizontal, x0, l.Y0, x1, l.Y0})
		}
	}
	return edges
}

// SnapEdges 将相近的平行边聚合到它们的平均位置。
// PDF 中的表格边框可能由多条略有偏移的线段组成，
// Snap 将这些偏移的线段"捕捉"到同一位置（它们的平均值）。
func SnapEdges(edges []Edge, tolerance float64) []Edge {
	if len(edges) == 0 {
		return nil
	}
	// 分别处理水平和垂直边
	var hEdges, vEdges []Edge
	for _, e := range edges {
		if e.Orientation == Horizontal {
			hEdges = append(hEdges, e)
		} else {
			vEdges = append(vEdges, e)
		}
	}
	var result []Edge
	// 水平边按 Y 坐标捕捉
	result = append(result, snapParallel(hEdges, tolerance, func(e Edge) float64 { return e.Y0 }, func(e Edge, v float64) Edge {
		dy := v - e.Y0
		return Edge{e.Orientation, e.X0, e.Y0 + dy, e.X1, e.Y1 + dy}
	})...)
	// 垂直边按 X 坐标捕捉
	result = append(result, snapParallel(vEdges, tolerance, func(e Edge) float64 { return e.X0 }, func(e Edge, v float64) Edge {
		dx := v - e.X0
		return Edge{e.Orientation, e.X0 + dx, e.Y0, e.X1 + dx, e.Y1 + dx}
	})...)
	return result
}

// snapParallel 对同方向的边按指定坐标进行聚类捕捉。
// 算法：按坐标排序 → 将相邻（差值 ≤ tolerance）的边分为一组 → 每组替换为平均值
func snapParallel(edges []Edge, tolerance float64, coordFn func(Edge) float64, replaceFn func(Edge, float64) Edge) []Edge {
	if len(edges) == 0 {
		return nil
	}
	// 按坐标排序
	sorted := make([]Edge, len(edges))
	copy(sorted, edges)
	sort.Slice(sorted, func(i, j int) bool {
		return coordFn(sorted[i]) < coordFn(sorted[j])
	})

	// 聚类：将相近的边分为一组
	var clusters [][]Edge
	var cur []Edge
	cur = append(cur, sorted[0])
	for i := 1; i < len(sorted); i++ {
		prevCoord := coordFn(cur[len(cur)-1])
		curCoord := coordFn(sorted[i])
		if curCoord-prevCoord <= tolerance {
			cur = append(cur, sorted[i])
		} else {
			clusters = append(clusters, cur)
			cur = []Edge{sorted[i]}
		}
	}
	clusters = append(clusters, cur)

	// 每组替换为平均值
	var result []Edge
	for _, cluster := range clusters {
		sum := 0.0
		for _, e := range cluster {
			sum += coordFn(e)
		}
		mean := sum / float64(len(cluster))
		for _, e := range cluster {
			result = append(result, replaceFn(e, mean))
		}
	}
	return result
}

// JoinEdges 合并共线的重叠或相邻边。
// 当两条同方向的边在垂直方向上位置相同（或接近）且在水平方向上重叠时，
// 将它们合并为一条更长的边。
func JoinEdges(edges []Edge, tolerance float64) []Edge {
	if len(edges) == 0 {
		return nil
	}
	var hEdges, vEdges []Edge
	for _, e := range edges {
		if e.Orientation == Horizontal {
			hEdges = append(hEdges, e)
		} else {
			vEdges = append(vEdges, e)
		}
	}
	var result []Edge
	result = append(result, joinParallel(hEdges, tolerance,
		func(e Edge) float64 { return e.Y0 },                       // 水平边按 Y 坐标分组
		func(e Edge) (float64, float64) { return e.X0, e.X1 },      // 获取 X 范围
		func(e Edge, x0, x1 float64) Edge {                          // 替换 X 范围
			return Edge{e.Orientation, x0, e.Y0, x1, e.Y1}
		},
	)...)
	result = append(result, joinParallel(vEdges, tolerance,
		func(e Edge) float64 { return e.X0 },                        // 垂直边按 X 坐标分组
		func(e Edge) (float64, float64) { return e.Y0, e.Y1 },      // 获取 Y 范围
		func(e Edge, y0, y1 float64) Edge {                          // 替换 Y 范围
			return Edge{e.Orientation, e.X0, y0, e.X1, y1}
		},
	)...)
	return result
}

// joinParallel 合并同方向、共线且重叠的边。
func joinParallel(edges []Edge, tolerance float64,
	coordFn func(Edge) float64,
	spanFn func(Edge) (float64, float64),
	replaceFn func(Edge, float64, float64) Edge,
) []Edge {
	if len(edges) == 0 {
		return nil
	}
	// 按位置坐标分组
	type group struct {
		coord float64
		edges []Edge
	}
	var groups []group
	for _, e := range edges {
		found := false
		for gi := range groups {
			if math.Abs(coordFn(e)-groups[gi].coord) <= tolerance {
				groups[gi].edges = append(groups[gi].edges, e)
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, group{coord: coordFn(e), edges: []Edge{e}})
		}
	}

	// 对每组内的边进行区间合并
	var result []Edge
	for _, g := range groups {
		var intervals []ivPair
		for _, e := range g.edges {
			lo, hi := spanFn(e)
			intervals = append(intervals, ivPair{lo, hi})
		}
		merged := mergeIntervals(intervals, tolerance)
		tpl := g.edges[0] // 使用第一条边作为模板（保留方向等信息）
		for _, iv := range merged {
			result = append(result, replaceFn(tpl, iv.lo, iv.hi))
		}
	}
	return result
}

// ivPair 表示一个闭区间 [lo, hi]
type ivPair struct{ lo, hi float64 }

// mergeIntervals 合并重叠或相邻的区间。
// 类似于 LeetCode "Merge Intervals" 问题。
func mergeIntervals(ivs []ivPair, tol float64) []ivPair {
	if len(ivs) == 0 {
		return nil
	}
	sort.Slice(ivs, func(i, j int) bool { return ivs[i].lo < ivs[j].lo })
	merged := []ivPair{ivs[0]}
	for _, iv := range ivs[1:] {
		last := &merged[len(merged)-1]
		if iv.lo <= last.hi+tol {
			// 重叠或相邻，合并
			if iv.hi > last.hi {
				last.hi = iv.hi
			}
		} else {
			merged = append(merged, iv)
		}
	}
	return merged
}
