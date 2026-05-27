#!/usr/bin/env python3
"""使用 pdf2docx 提取 PDF 并输出文本内容"""

import sys
import os

try:
    from pdf2docx import PyMuPDFConverter as Converter
except ImportError:
    try:
        from pdf2docx import Converter
    except ImportError:
        print("Error: pdf2docx not installed")
        print("Install with: pip install pdf2docx")
        sys.exit(1)


def extract_pdf(pdf_path):
    """提取 PDF 内容并返回文本"""
    cv = Converter(pdf_path)

    # 解析 PDF with default settings (no OCR)
    print("Parsing PDF...")
    cv.parse(ocr=False)

    # 获取所有页面
    page_count = len(cv.pages)
    print(f"Total pages: {page_count}")

    for page_num in range(page_count):
        page = cv.pages[page_num]
        print(f"\n{'='*60}")
        print(f"PAGE {page_num + 1}")
        print(f"{'='*60}")

        # 获取页面宽度
        width = page.width
        print(f"Page width: {width}")

        # 获取所有块
        blocks = page.blocks
        print(f"Block count: {len(blocks)}")

        for block_idx, block in enumerate(blocks):
            if hasattr(block, 'text') and block.text:
                text = block.text.strip()
                if text:
                    col_idx = getattr(block, 'col_idx', '?')
                    print(f"\n--- Block {block_idx} (Col={col_idx}) ---")
                    print(f"X: {block.x0:.1f} - {block.x1:.1f} Y: {block.y0:.1f} - {block.y1:.1f}")
                    print(f"Text: {text[:300]}..." if len(text) > 300 else f"Text: {text}")

    cv.close()


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(f"Usage: python {sys.argv[0]} <pdf_file>")
        sys.exit(1)

    pdf_path = sys.argv[1]
    if not os.path.exists(pdf_path):
        print(f"File not found: {pdf_path}")
        sys.exit(1)

    print(f"Extracting: {pdf_path}")
    extract_pdf(pdf_path)