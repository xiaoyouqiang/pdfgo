package model

import "math"

// Point 表示二维平面上的一个点，坐标采用 PDF 页面坐标系
//（原点在左下角，X 轴向右，Y 轴向上）
type Point struct {
	X, Y float64
}

// Rect 表示一个轴对齐的矩形区域（边界框）。
// 坐标系为 PDF 标准坐标系（Y0 在下方，Y1 在上方）。
type Rect struct {
	X0, Y0 float64 // 左下角坐标
	X1, Y1 float64 // 右上角坐标
}

// Width 返回矩形的宽度
func (r Rect) Width() float64 { return r.X1 - r.X0 }

// Height 返回矩形的高度
func (r Rect) Height() float64 { return r.Y1 - r.Y0 }

// MidX 返回矩形中心的 X 坐标
func (r Rect) MidX() float64 { return (r.X0 + r.X1) / 2 }

// MidY 返回矩形中心的 Y 坐标
func (r Rect) MidY() float64 { return (r.Y0 + r.Y1) / 2 }

// Contains 判断点 p 是否在矩形内部（包含边界）
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X0 && p.X <= r.X1 && p.Y >= r.Y0 && p.Y <= r.Y1
}

// Empty 判断矩形是否为空（宽度或高度 ≤ 0）
func (r Rect) Empty() bool { return r.X0 >= r.X1 || r.Y0 >= r.Y1 }

// Area 返回矩形的面积，空矩形返回 0
func (r Rect) Area() float64 {
	w := r.Width()
	h := r.Height()
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// Overlap 计算与另一个矩形的重叠比例。
// 返回值为重叠面积占当前矩形面积的比例，范围 [0, 1]。
// 用于判断两个文本框是否属于同一段落。
func (r Rect) Overlap(other Rect) float64 {
	if r.Empty() || other.Empty() {
		return 0
	}
	// 计算交集矩形的坐标
	x0 := math.Max(r.X0, other.X0)
	y0 := math.Max(r.Y0, other.Y0)
	x1 := math.Min(r.X1, other.X1)
	y1 := math.Min(r.Y1, other.Y1)
	w := math.Max(0, x1-x0)
	h := math.Max(0, y1-y0)
	area := w * h
	if area <= 0 {
		return 0
	}
	return area / r.Area()
}
