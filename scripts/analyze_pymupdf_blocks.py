#!/usr/bin/env python3
"""详细分析 PyMuPDF 的文本块结构"""

import sys
import os

sys.stdout.reconfigure(encoding='utf-8')

try:
    import fitz
except ImportError:
    print("Error: PyMuPDF not installed")
    print("Install with: pip install pymupdf")
    sys.exit(1)


def analyze_block_structure(page, block_idx, block):
    """分析单个块的结构"""
    if "lines" not in block:
        return

    print(f"\n{'='*60}")
    print(f"Block {block_idx} 分析")
    print(f"{'='*60}")
    print(f"BBox: X={block['bbox'][0]:.1f}-{block['bbox'][2]:.1f}, Y={block['bbox'][1]:.1f}-{block['bbox'][3]:.1f}")
    print(f"Lines: {len(block['lines'])}")

    # 分析每个 line
    for line_idx, line in enumerate(block["lines"]):
        spans_text = ""
        for span in line["spans"]:
            spans_text += span["text"]

        print(f"  Line {line_idx}: Y={line['bbox'][1]:.1f}-{line['bbox'][3]:.1f}, Text={spans_text[:50]}...")

        # 分析 line 内部的 span
        for span_idx, span in enumerate(line["spans"]):
            print(f"    Span {span_idx}: X={span['origin'][0]:.1f}, Font={span['font']}, Size={span['size']:.1f}, Text={span['text'][:30]}...")


def analyze_page_blocks(pdf_path, page_num=0):
    """分析指定页面的所有块"""
    doc = fitz.open(pdf_path)

    if page_num >= len(doc):
        print(f"Page {page_num} not found (total pages: {len(doc)})")
        doc.close()
        return

    page = doc[page_num]
    print(f"\n{'='*60}")
    print(f"分析 Page {page_num + 1}")
    print(f"Page size: {page.rect.width:.1f} x {page.rect.height:.1f}")
    print(f"{'='*60}")

    # 获取带结构的文本
    text_dict = page.get_text("dict")

    print(f"\n总 blocks 数: {len(text_dict['blocks'])}")

    for block_idx, block in enumerate(text_dict["blocks"][:15]):  # 只看前15个blocks
        if "lines" in block:
            analyze_block_structure(page, block_idx, block)

    doc.close()


def compare_with_go_output():
    """对比 Go 版本的输出"""
    print("\n" + "="*60)
    print("PyMuPDF 识别到的 Block 结构 (第一页):")
    print("="*60)

    doc = fitz.open("2605.23261v1.pdf")
    page = doc[0]
    text_dict = page.get_text("dict")

    for block_idx, block in enumerate(text_dict["blocks"][:15]):
        if "lines" not in block:
            continue

        # 计算 block 的实际 X 范围
        x0, y0, x1, y1 = block["bbox"]
        print(f"Block {block_idx}: X={x0:.1f}-{x1:.1f}, Y={y0:.1f}-{y1:.1f}, Lines={len(block['lines'])}")

        # 检查是否是左栏内容 (X < 300)
        if x0 < 200 and x1 < 350:
            print(f"  -> 左栏内容")
        elif x0 > 250:
            print(f"  -> 右栏内容")

    doc.close()


if __name__ == "__main__":
    pdf_path = "2605.23261v1.pdf"
    if not os.path.exists(pdf_path):
        pdf_path = sys.argv[1]

    print(f"分析: {pdf_path}")
    analyze_page_blocks(pdf_path, 0)
    compare_with_go_output()