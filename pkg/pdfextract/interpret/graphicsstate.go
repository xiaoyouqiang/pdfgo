package interpret

// GraphicsState 保存当前的图形状态，包括变换矩阵、颜色和线宽。
// 这些状态影响文本和图形在页面上的渲染位置和外观。
type GraphicsState struct {
	CTM         [6]float64 // 当前变换矩阵 [a b c d e f]，将用户空间坐标映射到设备空间
	FillColor   [3]float64 // 填充颜色（RGB，各分量 0-1）
	StrokeColor [3]float64 // 描边颜色（RGB，各分量 0-1）
	LineWidth   float64    // 线宽
}

// NewGraphicsState 创建默认的图形状态（单位矩阵，黑色填充/描边，1pt 线宽）
func NewGraphicsState() *GraphicsState {
	return &GraphicsState{
		CTM:         [6]float64{1, 0, 0, 1, 0, 0}, // 单位矩阵
		FillColor:   [3]float64{0, 0, 0},            // 黑色
		StrokeColor: [3]float64{0, 0, 0},             // 黑色
		LineWidth:   1.0,
	}
}

// ConcatMatrix 将当前变换矩阵（CTM）与给定矩阵相乘：CTM = CTM × m。
// 用于处理 PDF 的 cm 操作符，实现缩放、旋转、平移等变换。
//
// 矩阵格式：[a b c d e f] 表示：
//
//	| a  c  e |
//	| b  d  f |
//	| 0  0  1 |
func (g *GraphicsState) ConcatMatrix(m [6]float64) {
	ctm := g.CTM
	g.CTM = [6]float64{
		ctm[0]*m[0] + ctm[2]*m[1],       // 新 a
		ctm[1]*m[0] + ctm[3]*m[1],       // 新 b
		ctm[0]*m[2] + ctm[2]*m[3],       // 新 c
		ctm[1]*m[2] + ctm[3]*m[3],       // 新 d
		ctm[0]*m[4] + ctm[2]*m[5] + ctm[4], // 新 e（平移 X）
		ctm[1]*m[4] + ctm[3]*m[5] + ctm[5], // 新 f（平移 Y）
	}
}

// Transform 使用 CTM 将用户空间坐标 (x, y) 变换为设备/页面空间坐标。
// 变换公式：x' = a*x + c*y + e, y' = b*x + d*y + f
func (g *GraphicsState) Transform(x, y float64) (float64, float64) {
	ctm := g.CTM
	return ctm[0]*x + ctm[2]*y + ctm[4],
		ctm[1]*x + ctm[3]*y + ctm[5]
}

// Clone 返回图形状态的深拷贝，用于 q 操作符保存状态
func (g *GraphicsState) Clone() *GraphicsState {
	return &GraphicsState{
		CTM:         g.CTM,
		FillColor:   g.FillColor,
		StrokeColor: g.StrokeColor,
		LineWidth:   g.LineWidth,
	}
}

// GStateStack 实现 PDF 的 q/Q 图形状态栈。
// q 操作符将当前状态压栈，Q 操作符弹出恢复之前的状态。
type GStateStack struct {
	stack []*GraphicsState
}

// NewGStateStack 创建一个新的图形状态栈
func NewGStateStack() *GStateStack {
	return &GStateStack{}
}

// Push 保存当前图形状态到栈中（对应 q 操作符）
func (s *GStateStack) Push(g *GraphicsState) {
	s.stack = append(s.stack, g.Clone())
}

// Pop 从栈中恢复之前的图形状态（对应 Q 操作符）
func (s *GStateStack) Pop() *GraphicsState {
	if len(s.stack) == 0 {
		return nil
	}
	g := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	return g
}
