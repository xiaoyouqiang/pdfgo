#!/usr/bin/env python3
"""使用 pymupdf 直接提取 PDF 文本进行对比"""

import sys
import os

# 设置输出编码
sys.stdout.reconfigure(encoding='utf-8')

try:
    import fitz  # PyMuPDF
except ImportError:
    print("Error: PyMuPDF not installed")
    print("Install with: pip install pymupdf")
    sys.exit(1)


def extract_pdf(pdf_path):
    """提取 PDF 内容并返回文本"""
    doc = fitz.open(pdf_path)
    print(f"Total pages: {len(doc)}")

    for page_num in range(len(doc)):  # 全部页面
        page = doc[page_num]
        print(f"\n{'='*60}")
        print(f"PAGE {page_num + 1}")
        print(f"{'='*60}")

        width = page.rect.width
        height = page.rect.height
        print(f"Page size: {width:.1f} x {height:.1f}")

        # 获取文本块
        blocks = page.get_text("dict")["blocks"]
        print(f"Block count: {len(blocks)}")

        for block_idx, block in enumerate(blocks):
            if "lines" in block:
                block_text = ""
                for line in block["lines"]:
                    for span in line["spans"]:
                        block_text += span["text"]
                if block_text.strip():
                    print(f"\n--- Block {block_idx} ---")
                    print(f"X: {block['bbox'][0]:.1f} - {block['bbox'][2]:.1f}")
                    print(f"Y: {block['bbox'][1]:.1f} - {block['bbox'][3]:.1f}")
                    print(f"Text: {block_text.strip()}")

    doc.close()


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