package interpret

import (
	"math"

	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/font"
	"github.com/xiaoyouqiang/pdfgo/pkg/pdfextract/model"
)

// FormXObject 代表 PDF Form XObject 的内容，包含其内容流字节、字体解码器和嵌套的 Form XObject。
type FormXObject struct {
	Content      []byte                      // Form XObject 的内容流字节
	Fonts        map[string]font.FontDecoder // Form XObject 资源中的字体解码器
	FormXObjects map[string]*FormXObject     // Form 自身资源中的嵌套 Form XObject
	ObjNr        int                         // PDF 对象编号（用于递归保护）
	Matrix       [6]float64                  // Form XObject 的变换矩阵（默认为单位矩阵）
	HasMatrix    bool                        // 是否有显式 Matrix
}

// ContentInterpreter 是 PDF 内容流的核心解释器。
// 它解析 PDF 内容流中的操作符和操作数，维护图形和文本状态，
// 并生成带位置信息的字符（Char）、矩形（Rect）和线段（LineSegment）。
//
// PDF 内容流由一系列操作符组成，主要包括：
//   - 文本操作符（BT/ET、Tf、Tm、Tj、TJ 等）：控制文本的字体、位置和内容
//   - 图形状态操作符（q/Q、cm）：管理变换矩阵的保存/恢复和修改
//   - 路径操作符（re、m、l、S）：绘制矩形和线条，用于表格检测
//   - XObject 操作符（Do）：引用外部对象（如图片、Form XObject）
type ContentInterpreter struct {
	gState          *GraphicsState              // 当前图形状态（变换矩阵、颜色等）
	tState          *TextState                  // 当前文本状态（文本矩阵、字体等）
	gStack          *GStateStack                // 图形状态栈（q/Q 操作符使用）
	fonts           map[string]font.FontDecoder // 字体名称 → 解码器映射
	xObjects        map[string]int              // XObject 名称 → PDF 对象编号映射（仅 Image）
	formXObjects    map[string]*FormXObject     // XObject 名称 → Form XObject 映射
	result          *model.InterpretResult      // 解释结果（字符、矩形、线段、图片位置）
	processingObjs  map[int]bool                // 正在处理的 Form XObject 对象编号（防止无限递归）
	currentFormObjNr int                        // 当前正在处理的 Form XObject 对象编号（0=页面直接内容）
	charSeqNo       int                         // 字符绘制序号计数器（每个字符递增，用于双层渲染去重）

	pathPoints []model.Point // 当前路径的点集（用于表格线段检测）
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

// SetFormXObjects 设置 Form XObject 映射表。
// 用于 Do 操作符识别和递归解释 Form XObject 内容。
func (ci *ContentInterpreter) SetFormXObjects(forms map[string]*FormXObject) {
	ci.formXObjects = forms
}

// Interpret 解释 PDF 内容流，返回提取结果。
// 使用 Scanner 逐个读取 token，积累操作数，遇到操作符时执行对应逻辑。
func (ci *ContentInterpreter) Interpret(content []byte) *model.InterpretResult {
	ci.interpretContent(content)
	return ci.result
}

// interpretContent 解释一段内容流字节，将结果追加到 ci.result。
func (ci *ContentInterpreter) interpretContent(content []byte) {
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
}

// dispatch 根据操作符名称分派到对应的处理逻辑。
func (ci *ContentInterpreter) dispatch(op string, operands []Operand) {
	switch op {
	// --- 文本状态操作符 ---
	case "BT":
		// 开始文本块，重置文本状态但保留字体设置
		// 兼容某些 PDF 生成器将字体设置和文本显示放在不同 BT/ET 块的模式
		savedFont := ci.tState.Tf
		savedFontSize := ci.tState.Tfs
		ci.tState = NewTextState()
		if savedFont != "" {
			ci.tState.Tf = savedFont
			ci.tState.Tfs = savedFontSize
		}
	case "ET":
		// 结束文本块，保留字体设置
		savedFont := ci.tState.Tf
		savedFontSize := ci.tState.Tfs
		ci.tState = NewTextState()
		ci.tState.Tf = savedFont
		ci.tState.Tfs = savedFontSize
	case "Tf":
		if len(operands) >= 2 {
			if nameVal, ok := operands[0].Value.(string); ok {
				ci.tState.Tf = nameVal
			}
			if sizeVal, ok := operands[1].Value.(float64); ok {
				ci.tState.Tfs = sizeVal
			}
		}
	case "Tm":
		if len(operands) >= 6 {
			nums, ok := toFloats(operands)
			if ok && len(nums) >= 6 {
				ci.tState.SetMatrix([6]float64{nums[0], nums[1], nums[2], nums[3], nums[4], nums[5]})
			}
		}
	case "Td":
		if len(operands) >= 2 {
			nums, _ := toFloats(operands)
			if len(nums) >= 2 {
				ci.tState.MoveText(nums[0], nums[1])
			}
		}
	case "TD":
		if len(operands) >= 2 {
			nums, _ := toFloats(operands)
			if len(nums) >= 2 {
				ci.tState.MoveTextLeading(nums[0], nums[1])
			}
		}
	case "T*":
		ci.tState.NextLine()
	case "Tj":
		if len(operands) >= 1 {
			if data, ok := toBytes(operands[0]); ok {
				ci.showString(data)
			}
		}
	case "TJ":
		ci.showStrings(operands)
	case "'":
		ci.tState.NextLine()
		if len(operands) >= 1 {
			if data, ok := toBytes(operands[0]); ok {
				ci.showString(data)
			}
		}
	case "\"":
		if len(operands) >= 3 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Tw = v
			}
			if v, ok := operands[1].Value.(float64); ok {
				ci.tState.Tc = v
			}
			ci.tState.NextLine()
			if data, ok := toBytes(operands[2]); ok {
				ci.showString(data)
			}
		}
	case "Tc":
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Tc = v
			}
		}
	case "Tw":
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Tw = v
			}
		}
	case "Tz":
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Th = v / 100.0
			}
		}
	case "TL":
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Tl = v
			}
		}
	case "Tr":
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Tr = int(v)
			}
		}
	case "Ts":
		if len(operands) >= 1 {
			if v, ok := operands[0].Value.(float64); ok {
				ci.tState.Ts = v
			}
		}

	// --- 图形状态操作符 ---
	case "q":
		ci.gStack.Push(ci.gState)
	case "Q":
		if restored := ci.gStack.Pop(); restored != nil {
			ci.gState = restored
		}
	case "cm":
		if len(operands) >= 6 {
			nums, _ := toFloats(operands)
			if len(nums) >= 6 {
				ci.gState.ConcatMatrix([6]float64{nums[0], nums[1], nums[2], nums[3], nums[4], nums[5]})
			}
		}

	// --- 路径操作符（用于表格边框检测） ---
	case "re":
		if len(operands) >= 4 {
			nums, _ := toFloats(operands)
			if len(nums) >= 4 {
				x0, y0 := ci.gState.Transform(nums[0], nums[1])
				x1, y1 := ci.gState.Transform(nums[0]+nums[2], nums[1]+nums[3])
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
		if len(operands) >= 2 {
			nums, _ := toFloats(operands)
			if len(nums) >= 2 {
				x, y := ci.gState.Transform(nums[0], nums[1])
				ci.pathPoints = []model.Point{{X: x, Y: y}}
			}
		}
	case "l":
		if len(operands) >= 2 {
			nums, _ := toFloats(operands)
			if len(nums) >= 2 {
				x, y := ci.gState.Transform(nums[0], nums[1])
				ci.pathPoints = append(ci.pathPoints, model.Point{X: x, Y: y})
			}
		}
	case "S":
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
		ci.pathPoints = nil

	// --- 裁剪路径操作符 ---
	case "W", "W*":
		ci.gState.ClipApplied = true
		ci.pathPoints = nil
	case "n":
		ci.pathPoints = nil

	// --- XObject 操作符 ---
	case "Do":
		if len(operands) >= 1 {
			if name, ok := operands[0].Value.(string); ok {
				if _, found := ci.formXObjects[name]; found {
					ci.interpretFormXObjectByName(name)
				} else {
					objNr, found := ci.xObjects[name]
					if found {
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
}

// interpretFormXObjectByName 根据名称递归解释 Form XObject 的内容流。
// 使用对象编号防止无限递归，并正确处理嵌套作用域中的 Form XObject 名称映射。
func (ci *ContentInterpreter) interpretFormXObjectByName(name string) {
	form := ci.formXObjects[name]
	if form == nil {
		return
	}

	// 使用对象编号防止无限递归（同一名称在不同作用域可能指向不同对象）
	if ci.processingObjs == nil {
		ci.processingObjs = make(map[int]bool)
	}
	objNr := form.ObjNr
	if objNr > 0 && ci.processingObjs[objNr] {
		return
	}
	if objNr > 0 {
		ci.processingObjs[objNr] = true
		defer func() { delete(ci.processingObjs, objNr) }()
	}

	// 保存图形状态
	ci.gStack.Push(ci.gState)
	savedGState := ci.gState

	// 保存并设置当前 Form 对象编号（用于标记字符来源）
	savedFormObjNr := ci.currentFormObjNr
	ci.currentFormObjNr = form.ObjNr
	defer func() { ci.currentFormObjNr = savedFormObjNr }()

	// 应用 Form 的变换矩阵
	if form.HasMatrix {
		ci.gState = NewGraphicsState()
		ci.gState.ConcatMatrix(savedGState.CTM)
		ci.gState.ConcatMatrix(form.Matrix)
	}

	// 使用作用域化的字体映射：Form 自身的字体优先于外层作用域
	// （同名字体在不同作用域可能指向不同的解码器，如外层 GBK vs 内层 Identity-H）
	if len(form.Fonts) > 0 {
		savedFonts := ci.fonts
		newFonts := make(map[string]font.FontDecoder, len(savedFonts)+len(form.Fonts))
		for k, v := range savedFonts {
			newFonts[k] = v
		}
		for k, v := range form.Fonts {
			newFonts[k] = v // Form 自身字体覆盖外层同名字体
		}
		ci.fonts = newFonts
		defer func() { ci.fonts = savedFonts }()
	}

	// 如果 Form 有嵌套的 Form XObject，替换当前映射以进入内层作用域
	if len(form.FormXObjects) > 0 {
		savedFormXObjects := ci.formXObjects
		// 创建新的映射：内层覆盖外层
		newForms := make(map[string]*FormXObject, len(savedFormXObjects)+len(form.FormXObjects))
		for k, v := range savedFormXObjects {
			newForms[k] = v
		}
		for k, v := range form.FormXObjects {
			newForms[k] = v
		}
		ci.formXObjects = newForms
		defer func() { ci.formXObjects = savedFormXObjects }()
	}

	// 递归解释 Form 的内容流
	ci.interpretContent(form.Content)

	// 恢复图形状态
	if restored := ci.gStack.Pop(); restored != nil {
		ci.gState = restored
	}
}

// showString 解码并显示一个字节字符串。
func (ci *ContentInterpreter) showString(data []byte) {
	f := ci.currentFont()
	if f == nil {
		return
	}
	runes, widths := f.Decode(data)

	effSize := ci.tState.Tfs * ci.tState.Th
	tm := ci.tState.Tm
	ctm := ci.gState.CTM

	pageSx := ctm[0]*tm[0] + ctm[2]*tm[1]
	pageSy := ctm[1]*tm[1] + ctm[3]*tm[3]

	charScale := math.Abs(pageSx) + math.Abs(pageSy)
	if charScale == 0 {
		charScale = 1
	}

	for i, r := range runes {
		adv := widths[i]
		if adv == 0 {
			adv = 0.5
		}

		x, y := ci.gState.Transform(ci.tState.Tm[4], ci.tState.Tm[5])

		displacementTx := adv * effSize
		charWidthPage := pageSx * displacementTx
		if math.Abs(charWidthPage) < 0.01 {
			charWidthPage = charScale * displacementTx
		}
		charHeightPage := math.Abs(pageSy) * effSize
		if charHeightPage < 0.01 {
			charHeightPage = charScale * effSize
		}

		bbox := model.Rect{
			X0: x,
			Y0: y - charHeightPage*0.2,
			X1: x + charWidthPage,
			Y1: y + charHeightPage*0.8,
		}

		ci.charSeqNo++
		ci.result.Chars = append(ci.result.Chars, model.Char{
			Text:      string(r),
			Origin:    model.Point{X: x, Y: y},
			BBox:      bbox,
			Font:      ci.buildFontInfo(),
			Advance:   charWidthPage,
			FormObjNr: ci.currentFormObjNr,
			SeqNo:     ci.charSeqNo,
			Clipped:   ci.gState.ClipApplied,
		})

		ci.tState.Tm[4] += ci.tState.Tm[0] * displacementTx
		ci.tState.Tm[5] += ci.tState.Tm[1] * displacementTx
	}
}

// showStrings 处理 TJ 操作符，支持混合文本和字距调整。
func (ci *ContentInterpreter) showStrings(operands []Operand) {
	for _, op := range operands {
		if arr, ok := op.Value.([]any); ok {
			for _, elem := range arr {
				switch v := elem.(type) {
				case float64:
					ci.tState.Tm[4] += ci.tState.Tm[0] * (-v / 1000.0)
					ci.tState.Tm[5] += ci.tState.Tm[1] * (-v / 1000.0)
				case []byte:
					ci.showString(v)
				}
			}
		} else if data, ok := toBytes(op); ok {
			ci.showString(data)
		}
	}
}

func (ci *ContentInterpreter) currentFont() font.FontDecoder {
	if ci.tState.Tf == "" {
		return nil
	}
	if f, ok := ci.fonts[ci.tState.Tf]; ok {
		return f
	}
	return nil
}

func (ci *ContentInterpreter) buildFontInfo() model.FontInfo {
	return model.FontInfo{
		Name:       ci.tState.Tf,
		Size:       ci.tState.Tfs,
		Color:      ci.gState.FillColor,
		RenderMode: ci.tState.Tr,
	}
}

func toFloats(ops []Operand) ([]float64, bool) {
	var out []float64
	for _, op := range ops {
		if v, ok := op.Value.(float64); ok {
			out = append(out, v)
		}
	}
	return out, len(out) > 0
}

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
