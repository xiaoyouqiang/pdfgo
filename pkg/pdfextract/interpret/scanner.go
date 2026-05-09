// Package interpret 实现 PDF 内容流的词法分析和解释执行。
//
// PDF 内容流是由操作符和操作数组成的指令序列，用于描述页面上的文本、图形和图片。
// 本包将原始的内容流字节解码为结构化的字符、矩形和线段数据。
//
// 处理流程：
//   - Scanner: 词法分析器，将内容流字节切分为 token（数字、字符串、操作符等）
//   - Interpreter: 解释器，按 PDF 规范处理每个操作符，维护图形/文本状态
package interpret

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// TokenType 表示 token 的类型
type TokenType int

const (
	Number     TokenType = iota // 数字（整数或浮点数）
	Name                        // 名称对象，如 /FontName
	Literal                     // 字面量字符串，如 (Hello)
	HexLiteral                  // 十六进制字符串，如 <48656C6C6F>
	ArrayOpen                   // 数组开始 [
	ArrayClose                  // 数组结束 ]
	Operator                    // PDF 操作符，如 Tj、Tm、cm
	EOF                         // 内容流结束
)

// Token 表示词法分析器产生的一个 token
type Token struct {
	Type  TokenType
	Value any    // 值：Number 为 float64，Name/Literal/Operator 为 string，HexLiteral 为 []byte
	Op    string // 操作符名称（仅当 Type == Operator 时有效）
}

// Scanner 是 PDF 内容流的词法分析器，将原始字节切分为 token 序列。
// PDF 内容流使用后缀表示法：操作数在前，操作符在后，如 "72 200 Td"。
type Scanner struct {
	data []byte // 内容流原始数据
	pos  int    // 当前读取位置
}

// NewScanner 创建一个新的词法分析器
func NewScanner(data []byte) *Scanner {
	return &Scanner{data: data, pos: 0}
}

// PDF 中定义的空白字符（NUL、TAB、LF、FF、CR、SPACE）
var whitespace = []byte{0, 9, 10, 12, 13, 32}

// PDF 中定义的分隔符字符
var delimiters = []byte{'(', ')', '<', '>', '[', ']', '{', '}', '/', '%'}

// isWhitespace 判断字节是否为 PDF 空白字符
func isWhitespace(b byte) bool {
	for _, w := range whitespace {
		if b == w {
			return true
		}
	}
	return false
}

// isDelimiter 判断字节是否为 PDF 分隔符
func isDelimiter(b byte) bool {
	for _, d := range delimiters {
		if b == d {
			return true
		}
	}
	return false
}

// skipWhitespace 跳过空白字符和注释行（以 % 开头的行）
func (s *Scanner) skipWhitespace() {
	for s.pos < len(s.data) && isWhitespace(s.data[s.pos]) {
		s.pos++
	}
	// 跳过注释行
	for s.pos < len(s.data) && s.data[s.pos] == '%' {
		for s.pos < len(s.data) && s.data[s.pos] != 10 && s.data[s.pos] != 13 {
			s.pos++
		}
		for s.pos < len(s.data) && isWhitespace(s.data[s.pos]) {
			s.pos++
		}
	}
}

// Next 读取并返回下一个 token。
// 根据当前字符判断 token 类型并进行相应的解析。
func (s *Scanner) Next() (Token, error) {
	s.skipWhitespace()
	if s.pos >= len(s.data) {
		return Token{Type: EOF}, nil
	}

	c := s.data[s.pos]
	s.pos++

	switch c {
	case '%':
		// 注释行 — 跳到行尾后递归读取下一个 token
		for s.pos < len(s.data) && s.data[s.pos] != 10 && s.data[s.pos] != 13 {
			s.pos++
		}
		return s.Next()

	case '/':
		// 名称对象，如 /F1、/R7
		start := s.pos
		for s.pos < len(s.data) {
			b := s.data[s.pos]
			if isWhitespace(b) || isDelimiter(b) {
				break
			}
			s.pos++
		}
		name := string(s.data[start:s.pos])
		return Token{Type: Name, Value: name}, nil

	case '(':
		// 字面量字符串，支持嵌套括号和转义字符
		start := s.pos
		depth := 1 // 括号嵌套深度
		for s.pos < len(s.data) && depth > 0 {
			b := s.data[s.pos]
			if b == '\\' && s.pos+1 < len(s.data) {
				// 转义字符 — 跳过两个字节
				s.pos += 2
				continue
			}
			if b == '(' {
				depth++
			} else if b == ')' {
				depth--
			}
			s.pos++
		}
		str := string(s.data[start : s.pos-1])
		// 使用逐字符反转义，避免 ReplaceAll 的顺序问题
		str = unescapeLiteral(str)
		return Token{Type: Literal, Value: str}, nil

	case '<':
		// 可能是十六进制字符串 <...> 或字典 <<...>>
		start := s.pos
		if s.pos < len(s.data) && s.data[s.pos] == '<' {
			// << 字典标记 — 在内容流中不常见，回退并作为操作符处理
			s.pos--
			return s.scanOperator()
		}
		// 十六进制字符串：将十六进制字符解码为字节
		for s.pos < len(s.data) && s.data[s.pos] != '>' {
			s.pos++
		}
		s.pos++ // 跳过 >
		hex := string(s.data[start:s.pos-1])
		// 移除所有空白字符（空格、换行、回车、制表符等 PDF 规范允许的分隔符）
		hex = strings.Map(func(r rune) rune {
			if r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == '\f' {
				return -1
			}
			return r
		}, hex)
		// 奇数长度时补零
		if len(hex)%2 != 0 {
			hex += "0"
		}
		b := make([]byte, len(hex)/2)
		for i := 0; i < len(hex); i += 2 {
			hi, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
			b[i/2] = byte(hi)
		}
		return Token{Type: HexLiteral, Value: b}, nil

	case '>':
		if s.pos < len(s.data) && s.data[s.pos] == '>' {
			s.pos++
			return s.Next() // 跳过 >> 字典结束标记
		}
		return Token{}, errors.New("unexpected '>' in content stream")

	case '[':
		return Token{Type: ArrayOpen}, nil

	case ']':
		return Token{Type: ArrayClose}, nil

	case '+', '-', '.', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		// 数字 — 回退一个字符后扫描完整数字
		s.pos--
		return s.scanNumber()

	default:
		if unicode.IsLetter(rune(c)) || c == '*' || c == '\'' || c == '"' {
			// 操作符 — 回退并扫描
			s.pos--
			return s.scanOperator()
		}
		// 尝试作为数字处理
		s.pos--
		return s.scanNumber()
	}
}

// peek 查看下一个字节但不移动位置
func (s *Scanner) peek() byte {
	if s.pos < len(s.data) {
		return s.data[s.pos]
	}
	return 0
}

// unescapeLiteral 按顺序处理 PDF 字面量字符串的转义序列。
// 使用逐字符处理避免 ReplaceAll 的顺序依赖问题。
func unescapeLiteral(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i += 2
			case 'r':
				b.WriteByte('\r')
				i += 2
			case 't':
				b.WriteByte('\t')
				i += 2
			case 'b':
				b.WriteByte('\b')
				i += 2
			case 'f':
				b.WriteByte('\f')
				i += 2
			case '(':
				b.WriteByte('(')
				i += 2
			case ')':
				b.WriteByte(')')
				i += 2
			case '\\':
				b.WriteByte('\\')
				i += 2
			default:
				// 八进制转义 \ddd（1-3 位八进制数字）
				if s[i+1] >= '0' && s[i+1] <= '7' {
					val := 0
					j := i + 1
					for j < len(s) && j < i+4 && s[j] >= '0' && s[j] <= '7' {
						val = val*8 + int(s[j]-'0')
						j++
					}
					b.WriteByte(byte(val))
					i = j
				} else {
					// 未知转义，忽略反斜杠
					b.WriteByte(s[i+1])
					i += 2
				}
			}
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// scanNumber 扫描一个完整的数字（支持符号、小数点和科学计数法）
func (s *Scanner) scanNumber() (Token, error) {
	start := s.pos
	// 符号部分
	if s.pos < len(s.data) && (s.data[s.pos] == '+' || s.data[s.pos] == '-') {
		s.pos++
	}
	// 整数部分
	for s.pos < len(s.data) && s.data[s.pos] >= '0' && s.data[s.pos] <= '9' {
		s.pos++
	}
	// 小数点和小数部分
	if s.pos < len(s.data) && s.data[s.pos] == '.' {
		s.pos++
		for s.pos < len(s.data) && s.data[s.pos] >= '0' && s.data[s.pos] <= '9' {
			s.pos++
		}
	}
	// 科学计数法（e/E）
	if s.pos < len(s.data) && (s.data[s.pos] == 'e' || s.data[s.pos] == 'E') {
		s.pos++
		if s.pos < len(s.data) && (s.data[s.pos] == '+' || s.data[s.pos] == '-') {
			s.pos++
		}
		for s.pos < len(s.data) && s.data[s.pos] >= '0' && s.data[s.pos] <= '9' {
			s.pos++
		}
	}
	numStr := string(s.data[start:s.pos])
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return Token{}, fmt.Errorf("invalid number: %s: %w", numStr, err)
	}
	return Token{Type: Number, Value: val}, nil
}

// scanOperator 扫描一个操作符名称
func (s *Scanner) scanOperator() (Token, error) {
	start := s.pos
	for s.pos < len(s.data) && !isWhitespace(s.data[s.pos]) && !isDelimiter(s.data[s.pos]) {
		s.pos++
	}
	op := string(s.data[start:s.pos])
	return Token{Type: Operator, Op: op, Value: op}, nil
}

// ScanAll 读取所有 token 并返回切片
func (s *Scanner) ScanAll() ([]Token, error) {
	var tokens []Token
	for {
		tok, err := s.Next()
		if err != nil {
			return tokens, err
		}
		if tok.Type == EOF {
			break
		}
		tokens = append(tokens, tok)
	}
	return tokens, nil
}

// Operand 表示解析后的一个操作数（数字、字符串、名称或数组）
type Operand struct {
	Value any
}

// ScanOperandsAndOperator 从 token 切片中提取操作数和操作符。
// PDF 内容流使用后缀表示法，操作数在前，操作符在后。
// 例如：tokens [72, 200, Td] → operands=[72, 200], operator="Td"
func ScanOperandsAndOperator(tokens []Token) ([]Operand, string) {
	if len(tokens) == 0 {
		return nil, ""
	}
	var ops []Operand
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t.Type == Operator {
			// 遇到操作符，之前的所有 token 都是操作数
			return ops, t.Op
		}
		if t.Type == ArrayOpen {
			// 收集数组元素直到匹配的 ]
			var arr []any
			i++
			for i < len(tokens) && tokens[i].Type != ArrayClose {
				if tokens[i].Type == Number {
					arr = append(arr, tokens[i].Value.(float64))
				} else if tokens[i].Type == Literal {
					arr = append(arr, []byte(tokens[i].Value.(string)))
				} else if tokens[i].Type == HexLiteral {
					arr = append(arr, tokens[i].Value.([]byte))
				}
				i++
			}
			ops = append(ops, Operand{Value: arr})
		} else if t.Type == Number {
			ops = append(ops, Operand{Value: t.Value.(float64)})
		} else if t.Type == Literal {
			ops = append(ops, Operand{Value: []byte(t.Value.(string))})
		} else if t.Type == HexLiteral {
			ops = append(ops, Operand{Value: t.Value.([]byte)})
		} else if t.Type == Name {
			ops = append(ops, Operand{Value: t.Value.(string)})
		}
	}
	return ops, ""
}
