package docx

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ZeroHawkeye/wordZero/pkg/document"
)

// TitleLevel represents the heading level (1-6)
type TitleLevel int

const (
	NotATitle TitleLevel = 0
	Level1    TitleLevel = 1
	Level2    TitleLevel = 2
	Level3    TitleLevel = 3
	Level4    TitleLevel = 4
	Level5    TitleLevel = 5
	Level6    TitleLevel = 6
)

// Paragraph represents a paragraph in the document
type Paragraph struct {
	Level   TitleLevel
	Content string
	IsTable bool
}

// Table represents a table in the document
type Table struct {
	Rows [][]string
}

// Document represents a Word document
type Document struct {
	Paragraphs []Paragraph
	Tables     []Table
	Elements   []DocumentElement // Ordered mix of paragraphs and tables
	Images     []Image           // Extracted images
	ImagePath  string            // Output directory for images
}

// Image represents an extracted image from the document
type Image struct {
	ID            string // Relationship ID (e.g., "rId4")
	Filename      string // Original filename (e.g., "image1.jpeg")
	Data          []byte // Image binary data
	SavedFilename string // Actual saved filename (with UUID or prefix applied)
}

// isImagePlaceholder checks if text is an image placeholder
func isImagePlaceholder(text string) (string, bool) {
	if strings.HasPrefix(text, "{{IMAGE:") && strings.HasSuffix(text, "}}") {
		return text[9 : len(text)-2], true
	}
	return "", false
}

// titleFontList for Python-compatible font size detection (in points)
var titleFontList = [][]int{
	{36, 100}, // Level 1: 36pt+
	{26, 36},  // Level 2: 26-36pt
	{24, 26},  // Level 3: 24-26pt
	{22, 24},  // Level 4: 22-24pt
	{18, 22},  // Level 5: 18-22pt
	{16, 18},  // Level 6: 16-18pt
}

// getTitleLevelFromStyle determines title level from paragraph style
func getTitleLevelFromStyle(styleName string) TitleLevel {
	if styleName == "" {
		return NotATitle
	}

	// Try to match "Heading N" pattern
	re := regexp.MustCompile(`(?i)(?:heading|标题)(\d*)`)
	matches := re.FindStringSubmatch(styleName)
	if len(matches) >= 1 {
		// Check if it's a heading style
		lowerStyle := strings.ToLower(styleName)
		if strings.HasPrefix(lowerStyle, "heading") || strings.Contains(lowerStyle, "toc") || strings.HasPrefix(lowerStyle, "标题") {
			if len(matches) >= 2 && matches[1] != "" {
				var level int
				fmt.Sscanf(matches[1], "%d", &level)
				if level >= 1 && level <= 6 {
					return TitleLevel(level)
				}
			}
			// If no number but is a heading style, default to level 1
			if strings.HasPrefix(lowerStyle, "heading") || strings.Contains(lowerStyle, "toc") {
				return Level1
			}
			// Chinese 标题 without number
			if strings.HasPrefix(lowerStyle, "标题") {
				return Level1
			}
		}
	}

	// Handle pure numeric style values (Word uses "2", "3" for heading levels)
	if matched, _ := regexp.MatchString(`^\d+$`, styleName); matched {
		var level int
		fmt.Sscanf(styleName, "%d", &level)
		if level >= 1 && level <= 9 {
			return TitleLevel(level)
		}
	}

	return NotATitle
}

// getTitleLevelFromFontSize determines title level from font size and bold
func getTitleLevelFromFontSize(fontSizeStr string, isBold bool) TitleLevel {
	if fontSizeStr == "" || !isBold {
		return NotATitle
	}

	// Parse font size (could be string like "28" representing half-points)
	fontSize, err := strconv.Atoi(fontSizeStr)
	if err != nil || fontSize <= 0 {
		return NotATitle
	}

	// fontSize is in half-points, convert to points for comparison
	pt := fontSize / 2

	for i, threshold := range titleFontList {
		if pt >= threshold[0] && pt < threshold[1] {
			return TitleLevel(i + 1)
		}
	}
	return NotATitle
}

// getParagraphText extracts text content from a paragraph, with image placeholders
func getParagraphText(para *document.Paragraph, ridToFilename map[string]string) string {
	var result string
	for _, run := range para.Runs {
		if run.Drawing != nil {
			// Try to extract image rId from drawing
			if rid := extractImageRId(run.Drawing); rid != "" {
				if filename, ok := ridToFilename[rid]; ok {
					result += fmt.Sprintf("{{IMAGE:%s}}", filename)
				}
			}
			continue
		}
		result += run.Text.Content
	}
	return result
}

// extractImageRId extracts the relationship ID from a drawing element
func extractImageRId(drawing *document.DrawingElement) string {
	if drawing == nil {
		return ""
	}
	// Try inline drawing first
	if drawing.Inline != nil && drawing.Inline.Graphic != nil &&
		drawing.Inline.Graphic.GraphicData != nil &&
		drawing.Inline.Graphic.GraphicData.Pic != nil &&
		drawing.Inline.Graphic.GraphicData.Pic.BlipFill != nil &&
		drawing.Inline.Graphic.GraphicData.Pic.BlipFill.Blip != nil {
		return drawing.Inline.Graphic.GraphicData.Pic.BlipFill.Blip.Embed
	}
	// Try anchor drawing
	if drawing.Anchor != nil && drawing.Anchor.Graphic != nil &&
		drawing.Anchor.Graphic.GraphicData != nil &&
		drawing.Anchor.Graphic.GraphicData.Pic != nil &&
		drawing.Anchor.Graphic.GraphicData.Pic.BlipFill != nil &&
		drawing.Anchor.Graphic.GraphicData.Pic.BlipFill.Blip != nil {
		return drawing.Anchor.Graphic.GraphicData.Pic.BlipFill.Blip.Embed
	}
	return ""
}

// getCellText extracts text from a table cell, joining paragraphs with </br>
// Note: This function is currently unused as we use wordZero's Table.GetCellText method
func getCellText(cell *document.TableCell, ridToFilename map[string]string) string {
	if len(cell.Paragraphs) == 0 {
		return ""
	}

	var parts []string
	for _, para := range cell.Paragraphs {
		parts = append(parts, getParagraphText(&para, ridToFilename))
	}
	return strings.Join(parts, "</br>")
}

// ParseWordFile parses a Word document from a file path
func ParseWordFile(filePath string) (*Document, error) {
	// First, extract images directly from the ZIP archive
	images := extractImagesFromZip(filePath)

	// Build rId to filename map from relationships
	ridToFilename := buildRidToFilenameMap(filePath)

	// Then parse the document structure
	doc, err := document.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	result, err := parseWordZeroDocumentWithRidMap(doc, ridToFilename)
	if err != nil {
		return nil, err
	}

	result.Images = images
	return result, nil
}

// ParseWordDocument parses Word document from io.Reader
func ParseWordDocument(r io.Reader) (*Document, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read data: %w", err)
	}

	return ParseWordBytes(data)
}

// ParseWordBytes parses Word document from bytes
func ParseWordBytes(data []byte) (*Document, error) {
	// First, extract images directly from the ZIP archive
	images := extractImagesFromBytes(data)

	// Build rId to filename map from relationships in bytes
	ridToFilename := buildRidToFilenameMapFromBytes(data)

	// Then parse the document structure
	doc, err := document.OpenFromMemory(io.NopCloser(bytes.NewReader(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open document from bytes: %w", err)
	}

	result, err := parseWordZeroDocumentWithRidMap(doc, ridToFilename)
	if err != nil {
		return nil, err
	}

	result.Images = images
	return result, nil
}

// DocumentElement represents a paragraph or table in the document
type DocumentElement struct {
	IsParagraph bool
	Paragraph   *Paragraph
	Table       *Table
}

// buildRidToFilenameMap builds a map of relationship ID to filename from document relationships
// It reads the relationships XML directly from the ZIP archive since the field is unexported
func buildRidToFilenameMap(filePath string) map[string]string {
	ridToFilename := make(map[string]string)

	r, err := zip.OpenReader(filePath)
	if err != nil {
		return ridToFilename
	}
	defer r.Close()

	// Find and read the document relationships file (word/_rels/document.xml.rels)
	for _, f := range r.File {
		if f.Name == "word/_rels/document.xml.rels" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}

			// Parse the relationships XML
			ridToFilename = parseRelationshipsXML(string(data))
			break
		}
	}

	return ridToFilename
}

// buildRidToFilenameMapFromBytes builds a map of relationship ID to filename from document relationships (from bytes)
func buildRidToFilenameMapFromBytes(data []byte) map[string]string {
	ridToFilename := make(map[string]string)

	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ridToFilename
	}

	// Find and read the document relationships file
	for _, f := range r.File {
		if f.Name == "word/_rels/document.xml.rels" {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			xmlData, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			ridToFilename = parseRelationshipsXML(string(xmlData))
			break
		}
	}

	return ridToFilename
}

// parseRelationshipsXML parses the relationships XML and returns rId -> filename map
func parseRelationshipsXML(xmlContent string) map[string]string {
	ridToFilename := make(map[string]string)

	// Simple regex-based parsing for relationship elements
	// <Relationship Id="rId1" Type="...image..." Target="media/image1.jpeg"/>
	re := regexp.MustCompile(`<Relationship[^>]*Id="([^"]+)"[^>]*Type="[^"]*(?:image|picture)[^"]*"[^>]*Target="([^"]+)"[^>]*/?>`)
	matches := re.FindAllStringSubmatch(xmlContent, -1)

	for _, match := range matches {
		if len(match) >= 3 {
			rid := match[1]
			target := match[2]
			// Extract filename from target path like "media/image1.jpeg" or "word/media/image1.jpeg"
			filename := target
			if strings.Contains(target, "/") {
				parts := strings.Split(target, "/")
				filename = parts[len(parts)-1]
			}
			ridToFilename[rid] = filename
		}
	}

	// Also try the reverse pattern where Target comes before Type
	re2 := regexp.MustCompile(`<Relationship[^>]*Id="([^"]+)"[^>]*Target="([^"]+)"[^>]*Type="[^"]*(?:image|picture)[^"]*"[^>]*/?>`)
	matches2 := re2.FindAllStringSubmatch(xmlContent, -1)
	for _, match := range matches2 {
		if len(match) >= 3 {
			rid := match[1]
			target := match[2]
			if _, exists := ridToFilename[rid]; !exists {
				filename := target
				if strings.Contains(target, "/") {
					parts := strings.Split(target, "/")
					filename = parts[len(parts)-1]
				}
				ridToFilename[rid] = filename
			}
		}
	}

	return ridToFilename
}

// parseWordZeroDocumentWithRidMap converts a wordZero document to our Document format
func parseWordZeroDocumentWithRidMap(doc *document.Document, ridToFilename map[string]string) (*Document, error) {
	result := &Document{
		Paragraphs: make([]Paragraph, 0),
		Tables:     make([]Table, 0),
		Elements:   make([]DocumentElement, 0),
		Images:     make([]Image, 0),
	}

	for _, elem := range doc.Body.Elements {
		switch e := elem.(type) {
		case *document.Paragraph:
			//图片内容替换占位符逻辑也在这里
			text := getParagraphText(e, ridToFilename)

			// Skip TOC content
			if isTocContent(text) {
				continue
			}

			// Determine title level
			level := getTitleLevel(e, text)

			para := Paragraph{
				Level:   level,
				Content: text,
				IsTable: false,
			}
			result.Paragraphs = append(result.Paragraphs, para)
			result.Elements = append(result.Elements, DocumentElement{
				IsParagraph: true,
				Paragraph:   &result.Paragraphs[len(result.Paragraphs)-1],
			})

		case *document.Table:
			// Convert table to our format
			table := Table{
				Rows: make([][]string, 0),
			}

			for rowIdx := 0; rowIdx < e.GetRowCount(); rowIdx++ {
				row := make([]string, 0)
				for colIdx := 0; colIdx < e.GetColumnCount(); colIdx++ {
					cellText, err := e.GetCellText(rowIdx, colIdx)
					if err != nil {
						cellText = ""
					}
					row = append(row, cellText)
				}
				table.Rows = append(table.Rows, row)
			}

			result.Tables = append(result.Tables, table)
			result.Elements = append(result.Elements, DocumentElement{
				IsParagraph: false,
				Table:       &result.Tables[len(result.Tables)-1],
			})
		}
	}

	return result, nil
}

// extractImagesFromZip extracts images from a Word document ZIP archive (file path)
func extractImagesFromZip(filePath string) []Image {
	var images []Image

	r, err := zip.OpenReader(filePath)
	if err != nil {
		return images
	}
	defer r.Close()

	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "word/media/") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil || len(data) == 0 {
				continue
			}
			filename := filepath.Base(f.Name)
			images = append(images, Image{
				ID:       filename,
				Filename: filename,
				Data:     data,
			})
		}
	}
	return images
}

// extractImagesFromBytes extracts images from a Word document ZIP archive (byte array)
func extractImagesFromBytes(data []byte) []Image {
	var images []Image

	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return images
	}

	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "word/media/") {
			rc, err := f.Open()
			if err != nil {
				continue
			}
			imgData, err := io.ReadAll(rc)
			rc.Close()
			if err != nil || len(imgData) == 0 {
				continue
			}
			filename := filepath.Base(f.Name)
			images = append(images, Image{
				ID:       filename,
				Filename: filename,
				Data:     imgData,
			})
		}
	}
	return images
}

// isTocContent checks if the text is TOC content
func isTocContent(text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "toc") ||
		strings.Contains(lower, "hyperlink") ||
		strings.Contains(lower, "pager ef") ||
		strings.Contains(lower, "\\o") ||
		strings.Contains(lower, "\\h") ||
		strings.Contains(lower, "\\u") ||
		strings.Contains(lower, "_toc") ||
		strings.Contains(lower, "_ref") ||
		strings.Contains(lower, "_tab") ||
		lower == "目录" ||
		lower == "table of contents"
}

// getTitleLevel determines the title level of a paragraph
func getTitleLevel(para *document.Paragraph, text string) TitleLevel {
	// First check style
	if para.Properties != nil && para.Properties.ParagraphStyle != nil {
		styleName := para.Properties.ParagraphStyle.Val
		if level := getTitleLevelFromStyle(styleName); level != NotATitle {
			return level
		}
	}

	// Check font size + bold for title detection
	if len(para.Runs) > 0 {
		// Get the first run's font size
		for _, run := range para.Runs {
			if run.Properties != nil && run.Properties.FontSize != nil {
				fontSizeStr := run.Properties.FontSize.Val
				isBold := run.Properties.Bold != nil
				if level := getTitleLevelFromFontSize(fontSizeStr, isBold); level != NotATitle {
					return level
				}
				break // Only check first run's font size
			}
		}

		// Check if any run is bold and has significant text
		isBold := false
		for _, run := range para.Runs {
			if run.Properties != nil && run.Properties.Bold != nil {
				isBold = true
				break
			}
		}

		// Check for markdown-style heading
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "#") {
			count := 0
			for _, c := range text {
				if c == '#' {
					count++
				} else if c == ' ' || c == '\t' {
					break
				} else {
					count = 0
					break
				}
			}
			if count >= 1 && count <= 6 {
				return TitleLevel(count)
			}
		}

		// If text starts with number followed by dot and space (like "1. 审核目标"),
		// it's likely a title even without explicit heading style
		if isBold && len(text) > 0 && len(text) < 100 {
			re := regexp.MustCompile(`^\d+\.\s`)
			if re.MatchString(text) {
				return Level2 // Assume level 2 for numbered sections
			}
		}
	}

	return NotATitle
}

// ParseFromZip opens a Word document from a zip file
func ParseFromZip(filePath string) (*Document, error) {
	return ParseWordFile(filePath)
}

// SaveImages saves all extracted images to the specified directory
func (d *Document) SaveImages(outputDir string, prefix string, useUniqueName bool) error {
	if len(d.Images) == 0 {
		return nil
	}

	// Ensure directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	for i, img := range d.Images {
		filename := img.Filename
		// Apply unique name first (UUID), then prefix
		if useUniqueName {
			uid := generateUUID()
			ext := getFileExt(filename)
			filename = fmt.Sprintf("%s%s", uid, ext)
		}
		if prefix != "" {
			filename = prefix + filename
		}

		// Store the actual saved filename for later use in markdown replacement
		d.Images[i].SavedFilename = filename

		filepath := fmt.Sprintf("%s/%s", outputDir, filename)

		if err := os.WriteFile(filepath, img.Data, 0644); err != nil {
			return fmt.Errorf("failed to write image %d: %w", i, err)
		}
	}

	return nil
}

// GetImageMarkdown returns markdown with image placeholders replaced
func (d *Document) GetImageMarkdown(content string) string {
	ridToFilename := make(map[string]string)
	for _, img := range d.Images {
		ridToFilename[img.ID] = img.Filename
	}

	re := regexp.MustCompile(`\{\{IMAGE:([^}]+)\}\}`)
	return re.ReplaceAllStringFunc(content, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) >= 2 {
			rid := parts[1]
			if filename, ok := ridToFilename[rid]; ok {
				return fmt.Sprintf("![](%s)", filename)
			}
		}
		return match
	})
}

// GetImageMarkdownWithPrefix returns markdown with images saved and paths prefixed
func (d *Document) GetImageMarkdownWithPrefix(content string, prefix string, useUniqueName bool) string {
	ridToFilename := make(map[string]string)
	for _, img := range d.Images {
		// Use SavedFilename if already set (from SaveImages), otherwise generate new name
		filename := img.SavedFilename
		if filename == "" {
			// No SavedFilename, generate based on parameters
			filename = img.Filename
			if useUniqueName {
				uid := generateUUID()
				ext := getFileExt(filename)
				filename = fmt.Sprintf("%s%s", uid, ext)
			} else if prefix != "" {
				filename = prefix + filename
			}
		}
		ridToFilename[img.ID] = filename
	}

	re := regexp.MustCompile(`\{\{IMAGE:([^}]+)\}\}`)
	return re.ReplaceAllStringFunc(content, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if len(parts) >= 2 {
			rid := parts[1]
			if filename, ok := ridToFilename[rid]; ok {
				return fmt.Sprintf("![](%s)", filename)
			}
		}
		return match
	})
}

// generateUUID generates a random UUID
func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// getFileExt extracts file extension from filename
func getFileExt(filename string) string {
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			return filename[i:]
		}
	}
	return ""
}

// ToMarkdown converts the document to Markdown format, preserving order of paragraphs and tables
func (d *Document) ToMarkdown() string {
	var buf bytes.Buffer

	// Use Elements slice to preserve order
	if len(d.Elements) > 0 {
		for _, elem := range d.Elements {
			if elem.IsParagraph {
				para := elem.Paragraph
				content := para.Content
				if para.Level > NotATitle {
					markers := strings.Repeat("#", int(para.Level))
					buf.WriteString(fmt.Sprintf("%s %s\n\n", markers, content))
				} else {
					buf.WriteString(content)
					buf.WriteString("\n\n")
				}
			} else {
				buf.WriteString(elem.Table.ToMarkdown())
				buf.WriteString("\n")
			}
		}
	} else {
		// Fallback to separate lists (backward compatibility)
		for _, para := range d.Paragraphs {
			content := para.Content
			if para.Level > NotATitle {
				markers := strings.Repeat("#", int(para.Level))
				buf.WriteString(fmt.Sprintf("%s %s\n\n", markers, content))
			} else {
				buf.WriteString(content)
				buf.WriteString("\n\n")
			}
		}

		// Append tables
		for _, table := range d.Tables {
			buf.WriteString(table.ToMarkdown())
			buf.WriteString("\n")
		}
	}

	return buf.String()
}

// ToMarkdown converts a table to Markdown format
func (t *Table) ToMarkdown() string {
	var buf bytes.Buffer

	for i, row := range t.Rows {
		buf.WriteString("| ")
		buf.WriteString(strings.Join(row, " | "))
		buf.WriteString(" |\n")

		// Add separator after first row
		if i == 0 && len(row) > 0 {
			// Build separator cells: | --- | --- | ... | --- |
			buf.WriteString("| ")
			for j := 0; j < len(row); j++ {
				if j > 0 {
					buf.WriteString(" | ")
				}
				buf.WriteString("---")
			}
			buf.WriteString(" |\n")
		}
	}

	return buf.String()
}
