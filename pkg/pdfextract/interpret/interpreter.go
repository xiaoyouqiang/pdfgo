package interpret

import (
	"math"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/font"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// ContentInterpreter 是 PDF 内容流的核心解释器。
// 它解析 PDF 内容流中的操作符和操作数，维护图形和文本状态，
// 并生成带位置信息的字符（Char）、矩形（Rect）和线段（LineSegment）。
//
// PDF 内容流由一系列操作符组成，主要包括：
//   - 文本操作符（BT/ET、Tf、Tm、Tj、TJ 等）：控制文本的字体、位置和内容
//   - 图形状态操作符（q/Q、cm）：管理变换矩阵的保存/恢复和修改
//   - 路径操作符（re、m、l、S）：绘制矩形和线条，用于表格检测
//   - XObject 操作符（Do）：引用外部对象（如图片）
type ContentInterpreter struct {
	gState   *GraphicsState                  // 当前图形状态（变换矩阵、颜色等）
	tState   *TextState                      // 当前文本状态（文本矩阵、字体等）
	gStack   *GStateStack                    // 图形状态栈（q/Q 操作符使用）
	fonts    map[string]font.FontDecoder     // 字体名称 → 解码器映射
	xObjects map[string]int                  // XObject 名称 → PDF 对象编号映射
	result   *model.InterpretResult          // 解释结果（字符、矩形、线段、图片位置）

	pathPoints []model.Point                 // 当前路径的点集（用于表格线段检测）
}

// NewInterpreter 创建一个新的内容流解释器。
// fonts 参数提供字体名称到解码器的映射，用于将 PDF 字节编码转换为 Unicode 字符。
func NewInterpreter(fonts map[string]font.FontDecoder) *ContentInterpreter {
	if fonts == nil {
		fonts = make(map[string]font.FontDecoder)
	}
	return &ContentInterpreter{
		gState:   NewGraphicsState(),
		tState:   NewTextState(),
		gStack:   NewGStateStack(),
		fonts:    fonts,
		xObjects: make(map[string]int),
		result: &model.InterpretResult{
			Chars: []model.Char{},
			Rects: []model.Rect{},
			Lines: []model.LineSegment{},
		},
	}
}

// SetXObjects 设置 XObject 资源映射表（名称 → 对象编号）。
// 用于 Do 操作符识别和定位图片对象。
func (ci *ContentInterpreter) SetXObjects(xobjs map[string]int) {
	ci.xObjects = xobjs
}

// Interpret 解释 PDF 内容流，返回提取结果。
// 使用 Scanner 逐个读取 token，积累操作数，遇到操作符时执行对应逻辑。
func (ci *ContentInterpreter) Interpret(content []byte) *model.InterpretResult {
	s := NewScanner(content)
	var currentTokens []Token

	for {
		tok, err := s.Next()
		if err != nil {
			break
		}
		if tok.Type == EOF {
			break
		}

		currentTokens = append(currentTokens, tok)

		// 遇到操作符时，提取操作数并执行
		if tok.Type == Operator {
			operands, opName := ScanOperandsAndOperator(currentTokens)
			ci.dispatch(opName, operands)
			currentTokens = nil
		}
	}
	return ci.result
}

// dispatch 根据操作符名称分派到对应的处理逻辑。
// 处理 PDF 内容流中的所有主要操作符：
//   - 文本状态操作符（BT/ET/Tf/Tm/Td/TD/T*/Tj/TJ/'/"/Tc/Tw/Tz/TL/Tr/Ts）
//   - 图形状态操作符（q/Q/cm）
//   - 路径操作符（re/m/l/S/f/B）— 用于表格边框检测
//   - XObject 操作符（Do）— 用于图片位置记录
func (ci *ContentInterpreter) dispatch(op string, operands []Operand) {
	switch op {
	// --- 文本状态操作符 ---
	case "BT":
		// 开始文本块，重置文本状态
		ci.tState = NewTextState()
	case "ET":
		// 结束文本块，重置文本状态
		ci.tState = NewTextState()
	case "Tf":
		// 设置字体和字号：Tf fontName fontSize
		if len(operands) >= 2 {
			if nameVal, ok := operands[0].Value.(string); ok {
				ci.tState.Tf = nameVal
			}
			if sizeVal, ok := operands[1].Value.(float64); ok {
				ci.tState.Tfs = sizeVal
			}
		}
	case "Tm":
		// 设置文本矩阵：Tm a b c d e f
		if len(operands) >= 6 {
			nums, ok := toFloats(operands)
			if ok && len(nums) >= 6 {
				ci.tState.SetMatrix([6]float64{nums[0], nums[1], nums[2], nums[3], nums[4], nums[5]})
			}
		}
	case "Td":
		// 移动文本位置：Td tx ty
		if len(operands) >= 2 {
			nums, _ := toFloats(operands)
			if len(nums) >= 2 {
				ci.tState.MoveText(nums[0], nums[1])
			}
		}
	case "TD":
		// 移动文本位置并设置行距：TD tx ty
		if len(operands) >= 2 {
			nums, _ := toFloats(operands)
			if len(nums) >= 2 {
				ci.tState.MoveTextLeading(nums[0], nums[1])
			}
		}
	case "T*":
		// 移动到下一行
		ci.tState.NextLine()
	case "Tj":
		// 显示字符串：Tj (string)
		if len(operands) >= 1 {
			if data, ok := toBytes(operands[0]); ok {
				ci.showString(data)
			}
		}
	case "TJ":
		// 显示字符串数组（支持字距调整）：TJ [(text) -50 (more) 120 (text)]
		ci.showStrings(operands)
	case "'":
		// 移动到下一行并显示字符串
		ci.tState.NextLine()
		if len(operands) >= 1 {
			if data, ok := toBytes(operands[0]); ok {
				ci.showString(data)
			}
		}
	case "\"":
		// 设置间距、移动到下一行并显示字符串：" aw ac (string)
		if len(operands) >= 3 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Tw = v // 词间距
			}
			if v, ok := operands[1].Value.(float64); ok {
				ci.tState.Tc = v // 字符间距
			}
			ci.tState.NextLine()
			if data, ok := toBytes(operands[2]); ok {
				ci.showString(data)
			}
		}
	case "Tc":
		// 设置字符间距
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Tc = v
			}
		}
	case "Tw":
		// 设置词间距
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Tw = v
			}
		}
	case "Tz":
		// 设置水平缩放（百分比，除以 100 转为比例）
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Th = v / 100.0
			}
		}
	case "TL":
		// 设置文本行距
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Tl = v
			}
		}
	case "Tr":
		// 设置文本渲染模式
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Tr = int(v)
			}
		}
	case "Ts":
		// 设置文本上移量
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Ts = v
			}
		}

	// --- 图形状态操作符 ---
	case "q":
		// 保存当前图形状态到栈
		ci.gStack.Push(ci.gState)
	case "Q":
		// 从栈中恢复图形状态
		if restored := ci.gStack.Pop(); restored != nil {
			ci.gState = restored
		}
	case "cm":
		// 连接变换矩阵：cm a b c d e f
		// 将 CTM 与给定矩阵相乘，实现缩放、旋转、平移
		if len(operands) >= 6 {
			nums, _ := toFloats(operands)
			if len(nums) >= 6 {
				ci.gState.ConcatMatrix([6]float64{nums[0], nums[1], nums[2], nums[3], nums[4], nums[5]})
			}
		}

	// --- 路径操作符（用于表格边框检测） ---
	case "re":
		// 矩形路径：re x y width height
		// 将矩形转换到页面坐标系后记录，用于后续表格检测
		if len(operands) >= 4 {
			nums, _ := toFloats(operands)
			if len(nums) >= 4 {
				// 将矩形的两个角点从用户空间变换到页面空间
				x0, y0 := ci.gState.Transform(nums[0], nums[1])
				x1, y1 := ci.gState.Transform(nums[0]+nums[2], nums[1]+nums[3])
				// 确保坐标顺序正确（X0 < X1, Y0 < Y1）
				if x0 > x1 {
					x0, x1 = x1, x0
				}
				if y0 > y1 {
					y0, y1 = y1, y0
				}
				ci.result.Rects = append(ci.result.Rects, model.Rect{
					X0: x0, Y0: y0,
					X1: x1, Y1: y1,
				})
			}
		}
	case "m":
		// 移动到路径起点：m x y
		if len(operands) >= 2 {
			nums, _ := toFloats(operands)
			if len(nums) >= 2 {
				x, y := ci.gState.Transform(nums[0], nums[1])
				ci.pathPoints = []model.Point{{X: x, Y: y}}
			}
		}
	case "l":
		// 添加直线段到路径：l x y
		if len(operands) >= 2 {
			nums, _ := toFloats(operands)
			if len(nums) >= 2 {
				x, y := ci.gState.Transform(nums[0], nums[1])
				ci.pathPoints = append(ci.pathPoints, model.Point{X: x, Y: y})
			}
		}
	case "S":
		// 描边路径：将路径点转换为线段
		for i := 1; i < len(ci.pathPoints); i++ {
			ci.result.Lines = append(ci.result.Lines, model.LineSegment{
				X0: ci.pathPoints[i-1].X,
				Y0: ci.pathPoints[i-1].Y,
				X1: ci.pathPoints[i].X,
				Y1: ci.pathPoints[i].Y,
			})
		}
		ci.pathPoints = nil
	case "f", "f*", "B":
		// 填充路径：清除路径点（不产生线段，但路径结束）
		ci.pathPoints = nil

	// --- XObject 操作符 ---
	case "Do":
		// 绘制 XObject（如图片）：Do name
		// 记录图片在页面上的位置，通过 CTM 变换确定边界框
		if len(operands) >= 1 {
			if name, ok := operands[0].Value.(string); ok {
				objNr, found := ci.xObjects[name]
				if found {
					// 使用 CTM 将单位正方形 (0,0)-(1,1) 变换到页面空间
					p0x, p0y := ci.gState.Transform(0, 0)
					p1x, p1y := ci.gState.Transform(1, 1)
					x0, y0 := p0x, p0y
					x1, y1 := p1x, p1y
					if x0 > x1 { x0, x1 = x1, x0 }
					if y0 > y1 { y0, y1 = y1, y0 }
					ci.result.ImagePlacements = append(ci.result.ImagePlacements, model.ImagePlacement{
						Name:  name,
						ObjNr: objNr,
						BBox:  model.Rect{X0: x0, Y0: y0, X1: x1, Y1: y1},
					})
				}
			}
		}
	}
}

// showString 解码并显示一个字节字符串。
//
// 处理流程：
//  1. 使用当前字体解码器将字节转换为 Unicode 字符和宽度
//  2. 计算每个字符在页面空间中的位置和边界框
//  3. 更新文本矩阵，移动到下一个字符位置
//
// 字符位置计算：
//   - 页面坐标 = CTM × Tm × 字符位置
//   - 字符宽度 = 字体宽度 × 有效字号 × 页面缩放
func (ci *ContentInterpreter) showString(data []byte) {
	font := ci.currentFont()
	if font == nil {
		return
	}
	// 解码字节为 Unicode 字符和对应的宽度值
	runes, widths := font.Decode(data)

	// 计算有效字号（考虑水平缩放）
	effSize := ci.tState.Tfs * ci.tState.Th
	tm := ci.tState.Tm
	ctm := ci.gState.CTM

	// 计算组合缩放因子：页面空间位移 / 文本空间位移
	pageSx := ctm[0]*tm[0] + ctm[2]*tm[1] // 水平方向缩放
	pageSy := ctm[1]*tm[1] + ctm[3]*tm[3] // 垂直方向缩放

	// 处理旋转文本：当 pageSx 接近 0 时，使用组合缩放的绝对值之和作为字符尺寸
	charScale := math.Abs(pageSx) + math.Abs(pageSy)
	if charScale == 0 {
		charScale = 1
	}

	for i, r := range runes {
		adv := widths[i]
		if adv == 0 {
			adv = 0.5 // 未指定宽度时使用默认值
		}

		// 计算字符在页面空间中的基线起点位置
		x, y := ci.gState.Transform(ci.tState.Tm[4], ci.tState.Tm[5])

		// 计算字符在页面空间中的宽度和高度
		displacementTx := adv * effSize // 文本空间中的前进距离
		charWidthPage := pageSx * displacementTx
		if math.Abs(charWidthPage) < 0.01 {
			// 旋转文本：pageSx 接近 0，使用组合缩放估算尺寸
			charWidthPage = charScale * displacementTx
		}
		charHeightPage := math.Abs(pageSy) * effSize
		if charHeightPage < 0.01 {
			charHeightPage = charScale * effSize
		}

		// 估算字符的边界框（基于基线位置和大致高度比例）
		bbox := model.Rect{
			X0: x,
			Y0: y - charHeightPage*0.2,  // 基线下方约 20%
			X1: x + charWidthPage,
			Y1: y + charHeightPage*0.8,  // 基线上方约 80%
		}

		// 生成字符记录
		ci.result.Chars = append(ci.result.Chars, model.Char{
			Text:    string(r),
			Origin:  model.Point{X: x, Y: y},
			BBox:    bbox,
			Font:    ci.buildFontInfo(),
			Advance: charWidthPage,
		})

		// 更新文本矩阵，将光标移动到下一个字符位置
		// Tm = Tm × Translation(displacementTx, 0)
		ci.tState.Tm[4] += ci.tState.Tm[0] * displacementTx
		ci.tState.Tm[5] += ci.tState.Tm[1] * displacementTx
	}
}

// showStrings 处理 TJ 操作符，支持混合文本和字距调整。
// TJ 操作符的参数是一个数组，包含字符串和数字：
//   - 字符串：显示对应文本
//   - 数字：调整当前水平位置（负值向右移动，正值向左移动，单位为千分之一字号）
//
// 例如：[(Hello) -100 (World)] 表示在 "Hello" 和 "World" 之间增加 0.1 字号的间距
func (ci *ContentInterpreter) showStrings(operands []Operand) {
	for _, op := range operands {
		if arr, ok := op.Value.([]any); ok {
			for _, elem := range arr {
				switch v := elem.(type) {
				case float64:
					// 字距调整值：除以 1000 转为字号单位，负号调整方向
					ci.tState.Tm[4] += ci.tState.Tm[0] * (-v / 1000.0)
					ci.tState.Tm[5] += ci.tState.Tm[1] * (-v / 1000.0)
				case []byte:
					// 文本字符串：解码并显示
					ci.showString(v)
				}
			}
		} else if data, ok := toBytes(op); ok {
			ci.showString(data)
		}
	}
}

// currentFont 获取当前设置的字体解码器
func (ci *ContentInterpreter) currentFont() font.FontDecoder {
	if ci.tState.Tf == "" {
		return nil
	}
	if f, ok := ci.fonts[ci.tState.Tf]; ok {
		return f
	}
	return nil
}

// buildFontInfo 根据当前文本和图形状态构建字体信息
func (ci *ContentInterpreter) buildFontInfo() model.FontInfo {
	return model.FontInfo{
		Name:  ci.tState.Tf,      // 字体资源名称
		Size:  ci.tState.Tfs,     // 字号
		Color: ci.gState.FillColor, // 填充颜色（也是文字颜色）
	}
}

// toFloats 从操作数列表中提取所有 float64 值
func toFloats(ops []Operand) ([]float64, bool) {
	var out []float64
	for _, op := range ops {
		if v, ok := op.Value.(float64); ok {
			out = append(out, v)
		}
	}
	return out, len(out) > 0
}

// toBytes 从操作数中提取字节切片，支持 []byte 和 string 类型
func toBytes(op Operand) ([]byte, bool) {
	switch v := op.Value.(type) {
	case []byte:
		return v, true
	case string:
		return []byte(v), true
	default:
		return nil, false
	}
}
