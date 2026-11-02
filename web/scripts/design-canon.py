#!/usr/bin/env python3
"""
TG-Sender Design Canon — 设计规范检查脚本
用法: python scripts/design-canon.py
"""

import os
import re
import sys
from collections import defaultdict, Counter
from dataclasses import dataclass
from typing import List

ALLOWED_COLORS = {
    '#01030d', '#0a0e1a', '#020510',
    '#38bdf8', '#7dd3fc', '#0ea5e9', '#0284c7',
    '#a78bfa', '#c4b5fd', '#34d399', '#fb7185', '#fbbf24', '#f472b6',
    '#f0f4f8', '#f1f5f9', '#94a3b8', '#64748b',
    '#ffffff', '#fff', '#000000', '#000',
}

STARRY_SKY_COLORS = {
    '#050a1f', '#0a1628', '#0d1b3a', '#06122b', '#020a1f', '#04183a', '#082a5e', '#0e3d82',
    '#c7d4ff', '#a8c6ff', '#ffe9c7', '#ffd4a3', '#c7e0ff', '#a8d0ff',
}

ALLOWED_RADIUS = {'8px', '12px', '16px', '24px', '9999px', 'full', 'lg', 'xl', '2xl', '3xl', 'md', 'sm', 'none', 't', 'r', 'b', 'l', 'tl', 'tr', 'bl', 'br'}

SPACING_EXCEPTIONS = {1, 2, 100, 200, 300, 400, 500, 600, 700, 800, 900, 1000, 1024, 768, 640, 1280, 1920, 1080}
CANVAS_SIZE_EXCEPTIONS = {150, 250, 350, 450, 650, 950, 1250, 1550, 1850}

# Tailwind 标准类中的小数间距（合法）
TAILWIND_DECIMAL_SPACING = {'2.5', '3.5', '1.5', '0.5'}

IGNORE_PATTERNS = ['node_modules', 'dist', '.git', 'DESIGN.md']


@dataclass
class Issue:
    severity: str
    category: str
    file: str
    line: int
    detail: str
    suggestion: str


class DesignCanon:
    def __init__(self, src_path: str):
        self.src_path = src_path
        self.issues: List[Issue] = []
        self.stats = defaultdict(int)

    def scan(self):
        files = self._get_files()
        print(f"🔍 扫描 {len(files)} 个文件...")

        for filepath in files:
            self._scan_file(filepath)

        self._generate_report()

    def _get_files(self) -> List[str]:
        files = []
        for root, dirs, filenames in os.walk(self.src_path):
            dirs[:] = [d for d in dirs if d not in IGNORE_PATTERNS]
            for f in filenames:
                if f.endswith('.vue'):
                    files.append(os.path.join(root, f))
        return files

    def _scan_file(self, filepath: str):
        rel_path = filepath.replace(self.src_path, '').lstrip('/')
        is_starry = 'StarrySky' in rel_path or 'starry' in rel_path.lower()

        try:
            with open(filepath, 'r', encoding='utf-8') as f:
                lines = f.readlines()
        except Exception as e:
            return

        for line_num, line in enumerate(lines, 1):
            self._check_hardcoded_colors(line, rel_path, line_num, is_starry)
            self._check_spacing(line, rel_path, line_num, is_starry)
            self._check_radius(line, rel_path, line_num)
            self._check_font_sizes(line, rel_path, line_num)

    def _check_hardcoded_colors(self, line: str, file: str, line_num: int, is_starry: bool):
        # 只检查模板中的硬编码，排除 script 中的 Canvas/WebGL 代码
        if '<script' in line or 'const ' in line or 'let ' in line or 'var ' in line:
            return

        hex_colors = re.findall(r'#([0-9a-fA-F]{3,8})\b', line)
        for hex_val in hex_colors:
            full_hex = f'#{hex_val}'
            if full_hex.lower() not in ALLOWED_COLORS and len(hex_val) >= 3:
                if is_starry and full_hex.lower() in {c.lower() for c in STARRY_SKY_COLORS}:
                    continue
                if self._is_in_allowed_context(line):
                    continue

                self.issues.append(Issue(
                    severity='critical',
                    category='硬编码颜色',
                    file=file,
                    line=line_num,
                    detail=f'发现未注册的 hex 颜色: {full_hex}',
                    suggestion='请改用 Tailwind 主题变量'
                ))
                self.stats['hardcoded_hex'] += 1

        if not is_starry:
            rgba_colors = re.findall(r'rgba\([^)]+\)', line)
            for rgba in rgba_colors:
                if 'var(' not in line and '--' not in line and '${' not in line:
                    # 检查是否在 style 属性或 class 中
                    if 'style=' in line or 'class=' in line or 'bg-' in line or 'text-' in line:
                        self.issues.append(Issue(
                            severity='critical',
                            category='硬编码颜色',
                            file=file,
                            line=line_num,
                            detail=f'发现硬编码 rgba: {rgba[:40]}',
                            suggestion='请改用 Tailwind 透明度语法'
                        ))
                        self.stats['hardcoded_rgba'] += 1

    def _check_spacing(self, line: str, file: str, line_num: int, is_starry: bool):
        # 排除 script 段和 Canvas 参数
        if '<script' in line or 'camera' in line.lower() or 'position' in line.lower():
            return

        px_values = re.findall(r'(\d+)px', line)
        for val in px_values:
            num = int(val)
            if num in SPACING_EXCEPTIONS or num in CANVAS_SIZE_EXCEPTIONS:
                continue
            if is_starry and num > 50:
                continue
            if num % 4 != 0 and num > 0:
                self.issues.append(Issue(
                    severity='warning',
                    category='间距不规范',
                    file=file,
                    line=line_num,
                    detail=f'发现非 4px 倍数的间距: {num}px',
                    suggestion=f'建议改为 {num - (num % 4)}px 或 {num + (4 - num % 4)}px'
                ))
                self.stats['bad_spacing'] += 1

    def _check_radius(self, line: str, file: str, line_num: int):
        radius_classes = re.findall(r'rounded-(\w+)', line)
        for r in radius_classes:
            if r not in ALLOWED_RADIUS and not r.startswith('['):
                self.issues.append(Issue(
                    severity='warning',
                    category='圆角不规范',
                    file=file,
                    line=line_num,
                    detail=f'发现非标准圆角类: rounded-{r}',
                    suggestion='请使用 rounded-lg(8px)、rounded-xl(12px)、rounded-2xl(16px) 或 rounded-full'
                ))
                self.stats['bad_radius'] += 1

    def _check_font_sizes(self, line: str, file: str, line_num: int):
        bad_font_sizes = re.findall(r'text-\[(\d+(?:px|rem|em))\]', line)
        for size in bad_font_sizes:
            self.issues.append(Issue(
                severity='warning',
                category='字号不规范',
                file=file,
                line=line_num,
                detail=f'发现硬编码字号: text-[{size}]',
                suggestion='请使用 Tailwind 标准字号: text-xs(12px)、text-sm(14px)、text-base(16px)、text-lg(18px)、text-xl(20px)'
            ))
            self.stats['bad_font_size'] += 1

    def _is_in_allowed_context(self, line: str) -> bool:
        stripped = line.strip()
        if stripped.startswith('//') or stripped.startswith('*') or stripped.startswith('<!--'):
            return True
        if 'gradient' in line.lower() and ('from-' in line or 'to-' in line):
            return True
        return False

    def _generate_report(self):
        print(f"\n{'='*70}")
        print("  DESIGN CANON 规范检查报告")
        print(f"{'='*70}")

        if not self.issues:
            print("\n  🎉 恭喜！未发现任何规范违规项。")
            print(f"{'='*70}")
            return

        critical = [i for i in self.issues if i.severity == 'critical']
        warnings = [i for i in self.issues if i.severity == 'warning']

        print(f"\n  总计发现问题: {len(self.issues)} 处")
        print(f"    🔴 Critical (必须修复): {len(critical)} 处")
        print(f"    🟡 Warning (建议修复): {len(warnings)} 处")

        print(f"\n  按类别统计:")
        category_counts = Counter(i.category for i in self.issues)
        for cat, count in category_counts.most_common():
            print(f"    {cat}: {count} 处")

        if critical:
            print(f"\n{'─'*70}")
            print("  🔴 CRITICAL — 必须修复")
            print(f"{'─'*70}")
            for i in critical[:20]:
                print(f"\n  [{i.file}:{i.line}]")
                print(f"    问题: {i.detail}")
                print(f"    建议: {i.suggestion}")
            if len(critical) > 20:
                print(f"\n  ... 还有 {len(critical)-20} 处")

        if warnings:
            print(f"\n{'─'*70}")
            print("  🟡 WARNING — 建议修复")
            print(f"{'─'*70}")
            for i in warnings[:10]:
                print(f"\n  [{i.file}:{i.line}]")
                print(f"    问题: {i.detail}")
                print(f"    建议: {i.suggestion}")
            if len(warnings) > 10:
                print(f"\n  ... 还有 {len(warnings)-10} 处")

        print(f"\n{'='*70}")

        if critical:
            sys.exit(1)
        sys.exit(0)


def main():
    script_dir = os.path.dirname(os.path.abspath(__file__))
    project_root = os.path.dirname(script_dir)
    src_path = os.path.join(project_root, 'src')

    if not os.path.exists(src_path):
        print(f"❌ 找不到 src 目录: {src_path}")
        sys.exit(1)

    canon = DesignCanon(src_path)
    canon.scan()


if __name__ == '__main__':
    main()
