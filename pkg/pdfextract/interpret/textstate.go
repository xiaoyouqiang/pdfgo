package interpret

// TextState 保存当前文本状态，包括文本矩阵、字体信息和各种间距参数。
// 这些状态由 PDF 文本操作符（BT/ET 块内）设置和修改。
//
// PDF 文本定位系统使用两个矩阵：
//   - Tm（文本矩阵）：当前文本位置的变换矩阵
//   - Tlm（文本行矩阵）：记录当前行的起始位置，用于换行计算
//
// 文本在页面上的最终位置 = CTM × Tm
type TextState struct {
	Tm  [6]float64 // 文本矩阵 [a b c d e f]，控制文本的缩放、旋转和位置
	Tlm [6]float64 // 文本行矩阵，记录当前文本行的起始状态
	Tf  string     // 当前字体资源名称（如 "F1"、"/SimSun"）
	Tfs float64    // 字体大小（单位：磅 pt）
	Tc  float64    // 字符间距（Tc 操作符设置）
	Tw  float64    // 词间距（Tw 操作符设置，仅对空格字符生效）
	Th  float64    // 水平缩放因子（Tz 操作符设置，百分比/100，默认 1.0）
	Tl  float64    // 文本行距（TL 操作符设置，用于 T* 和 TD 操作符）
	Tr  int        // 文本渲染模式（Tr 操作符设置：0=填充，1=描边，2=填充+描边，3=不可见等）
	Ts  float64    // 文本上移量（Ts 操作符设置）
}

// NewTextState 创建默认的文本状态。
// 文本矩阵和行矩阵初始化为单位矩阵，水平缩放默认为 100%。
func NewTextState() *TextState {
	return &TextState{
		Tm:  [6]float64{1, 0, 0, 1, 0, 0}, // 单位矩阵
		Tlm: [6]float64{1, 0, 0, 1, 0, 0}, // 单位矩阵
		Th:  1.0,                           // 100% 水平缩放
		Tr:  0,                             // 填充模式
	}
}

// SetMatrix 设置文本矩阵（Tm 操作符），同时将行矩阵重置为相同值。
// 通常在 BT/ET 块开头或 Tm 操作符时调用。
func (t *TextState) SetMatrix(m [6]float64) {
	t.Tm = m
	t.Tlm = m
}

// MoveText 按指定偏移量移动文本位置（Td 操作符）。
// 使用文本行矩阵计算新位置，模拟在当前行内的相对移动。
//
// 计算：Tlm = Translation(tx, ty) × Tlm; Tm = Tlm
func (t *TextState) MoveText(tx, ty float64) {
	t.Tlm[4] += t.Tlm[0]*tx + t.Tlm[2]*ty // 更新行矩阵的平移 X
	t.Tlm[5] += t.Tlm[1]*tx + t.Tlm[3]*ty // 更新行矩阵的平移 Y
	t.Tm = t.Tlm                           // 文本矩阵同步为行矩阵
}

// NextLine 移动到下一行的起始位置（T* 操作符）。
// 等价于 MoveText(0, -Tl)，即向下移动一个行距。
func (t *TextState) NextLine() {
	t.Tlm[4] += t.Tlm[2] * (-t.Tl) // 向下移动（注意 PDF Y 轴向上）
	t.Tlm[5] += t.Tlm[3] * (-t.Tl)
	t.Tm = t.Tlm
}

// MoveTextLeading 移动文本位置并同时设置行距（TD 操作符）。
// TD 操作符将行距设置为 ty 的相反数，然后执行 Td 移动。
func (t *TextState) MoveTextLeading(tx, ty float64) {
	t.Tl = -ty // 设置行距为 -ty
	t.MoveText(tx, ty)
}
