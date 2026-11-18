// Package split - clean.go：文本清理函数（自 ss-ai/pkg/tool 本地化，
// pdfgo 无 tool 包，ClearSplitResult 所需的最小集合）。
package split

import (
	"golang.org/x/net/html"
	"regexp"
	"strings"
	"unicode"
)

// CleanText 去除所有符号但保留中英文、数字和空格
func CleanText(text string) string {
	var result []rune
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || // 中文
			unicode.IsLetter(r) || // 英文
			unicode.IsNumber(r) || // 数字
			unicode.IsSpace(r) { // 保留空格
			result = append(result, r)
		}
	}
	return string(result)
}

// CleanHTML 去除 HTML 标签、注释、样式脚本，标准化空白
func CleanHTML(htmlContent string) string {
	// 处理HTML字符实体
	text := html.UnescapeString(htmlContent)

	// 去除注释
	text = removeComments(text)

	// 去除样式和脚本
	text = removeStyleAndScript(text)

	// 去除所有HTML标签
	text = removeAllTags(text)

	// 标准化空白字符
	text = normalizeWhitespace(text)

	return strings.TrimSpace(text)
}

func removeComments(text string) string {
	re := regexp.MustCompile(`<!--[\s\S]*?-->`)
	return re.ReplaceAllString(text, "")
}

func removeStyleAndScript(text string) string {
	// 去除<style>标签内容
	re := regexp.MustCompile(`(?i)<style[\s\S]*?</style>`)
	text = re.ReplaceAllString(text, "")

	// 去除<script>标签内容
	re = regexp.MustCompile(`(?i)<script[\s\S]*?</script>`)
	text = re.ReplaceAllString(text, "")

	re = regexp.MustCompile(`(?i)<textarea[\s\S]*?</textarea>`)
	text = re.ReplaceAllString(text, "")

	re = regexp.MustCompile(`(?i)<meta[\s\S]*?/>`)

	return re.ReplaceAllString(text, "")
}

func removeAllTags(text string) string {
	// 解析HTML并提取纯文本
	doc, err := html.Parse(strings.NewReader(text))
	if err == nil {
		var buf strings.Builder
		extractText(doc, &buf)
		return buf.String()
	}

	// 如果解析失败，使用正则表达式作为后备方案
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(text, "")
}

func extractText(n *html.Node, buf *strings.Builder) {
	if n.Type == html.TextNode {
		buf.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractText(c, buf)
	}
}

func normalizeWhitespace(text string) string {
	// 合并多个空白字符为单个空格
	re := regexp.MustCompile(`\s+`)
	text = re.ReplaceAllString(text, " ")

	// 去除控制字符
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
}

// IsNumber 判断字符串是否全部由数字组成
func IsNumber(str string) bool {
	for _, r := range str {
		if !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}
