package table

import (
	"math"
	"sort"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// Intersection 表示水平边和垂直边的交叉点。
// 一个交叉点可能有多条水平边和垂直边通过，这些边的索引分别记录在 HEdges 和 VEdges 中。
type Intersection struct {
	Point  model.Point // 交叉点坐标
	HEdges []int       // 通过此点的水平边索引列表
	VEdges []int       // 通过此点的垂直边索引列表
}

// pointKey 用于交叉点去重的量化键
type pointKey struct {
	X, Y int
}

// snapKey 将浮点坐标量化为整数键（乘以 10 后四舍五入）
func snapKey(x, y float64) pointKey {
	return pointKey{int(math.Round(x * 10)), int(math.Round(y * 10))}
}

// FindIntersections 查找所有水平边和垂直边的交叉点。
// 对每条垂直边，检查它是否与每条水平边相交：
//   - 垂直边的 X 坐标在水平边的 X 范围内
//   - 水平边的 Y 坐标在垂直边的 Y 范围内
//
// 相近的交叉点会被合并（通过 snapKey 量化），并累积关联的所有边索引。
func FindIntersections(edges []Edge, xTol, yTol float64) []Intersection {
	type indexedEdge struct {
		Edge  Edge
		Index int
	}
	// 分离水平和垂直边
	var hEdges, vEdges []indexedEdge
	for i, e := range edges {
		if e.Orientation == Horizontal {
			hEdges = append(hEdges, indexedEdge{e, i})
		} else {
			vEdges = append(vEdges, indexedEdge{e, i})
		}
	}

	// 使用量化键聚合并去重交叉点
	type acc struct {
		pt     model.Point
		hEdges []int
		vEdges []int
	}
	m := make(map[pointKey]*acc)

	// 遍历所有垂直边和水平边的组合
	for _, v := range vEdges {
		for _, h := range hEdges {
			vx := v.Edge.X0 // 垂直边的 X 坐标
			hy := h.Edge.Y0 // 水平边的 Y 坐标

			// 检查交叉条件
			if vx < h.Edge.X0-xTol || vx > h.Edge.X1+xTol {
				continue // 垂直边 X 不在水平边范围内
			}
			if hy < v.Edge.Y0-yTol || hy > v.Edge.Y1+yTol {
				continue // 水平边 Y 不在垂直边范围内
			}

			// 记录交叉点
			k := snapKey(vx, hy)
			if m[k] == nil {
				m[k] = &acc{pt: model.Point{X: vx, Y: hy}}
			}
			m[k].hEdges = append(m[k].hEdges, h.Index)
			m[k].vEdges = append(m[k].vEdges, v.Index)
		}
	}

	// 转换为切片并排序
	result := make([]Intersection, 0, len(m))
	for _, a := range m {
		result = append(result, Intersection{
			Point:  a.pt,
			HEdges: a.hEdges,
			VEdges: a.vEdges,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Point.X != result[j].Point.X {
			return result[i].Point.X < result[j].Point.X
		}
		return result[i].Point.Y < result[j].Point.Y
	})
	return result
}
