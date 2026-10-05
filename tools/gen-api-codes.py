#!/usr/bin/env python3
"""扫描 Go 后端用户可见的静态中文 message/error,生成内容寻址错误码。

用法:
  python3 tools/gen-api-codes.py --dry   # 只统计
  python3 tools/gen-api-codes.py         # 就地插入 code 字段 + 输出映射 JSON

规则:
- 只处理完全静态的中文串("error": "中文…" / "message": "中文…" / "msg": "中文…")
- 同一响应块里已经有 "code":(auth 中间件等先例用语义化 code)的跳过插入,
  但把 既有code → 中文message 收进映射,前端一视同仁按 api.<code> 翻译
- 拼接/Sprintf/变量串跳过(那些文案后端为准,前端透传)
- 新 code = "E" + sha1(原文)[:8],内容寻址:同一文案跨 handler 复用同一 code
"""
import hashlib
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent / "server" / "internal"
FIELD_RE = re.compile(
    r'("(?:error|message|msg)"\s*:\s*)("(?:[^"\\]|\\.)*[\u4e00-\u9fff](?:[^"\\]|\\.)*")(\s*[,}])'
)
# 块内后续出现这些键之前遇到 code 键 → 认为该块已有 code
NEXT_FIELD_RE = re.compile(r'"(?:error|message|msg|code|success|data)"\s*:')

def stable_code(text: str) -> str:
    return "E" + hashlib.sha1(text.encode("utf-8")).hexdigest()[:8].upper()

def main() -> None:
    dry = "--dry" in sys.argv
    mapping: dict[str, str] = {}
    stats = {"inserted": 0, "existing": 0, "files": 0}

    for path in sorted(ROOT.rglob("*.go")):
        src = path.read_text(encoding="utf-8")
        out = []
        pos = 0
        touched = False

        for m in FIELD_RE.finditer(src):
            head, literal, tail = m.group(1), m.group(2), m.group(3)
            text = json.loads(literal)
            # 向后看:同一块内(到下一个同级字段或明显越界前)是否已有 code
            lookahead = src[m.end():m.end() + 240]
            has_code = False
            for fm in NEXT_FIELD_RE.finditer(lookahead):
                if lookahead[fm.start():fm.start() + 7].startswith('"code"'):
                    has_code = True
                break   # 只看紧邻的下一个字段
            if has_code:
                stats["existing"] += 1
                continue

            code = stable_code(text)
            mapping[code] = text
            out.append(src[pos:m.start()])
            out.append(f'{head}{literal}, "code": "{code}"{tail}')
            pos = m.end()
            stats["inserted"] += 1
            touched = True

        if touched:
            out.append(src[pos:])
            stats["files"] += 1
            if not dry:
                path.write_text("".join(out), encoding="utf-8")

    # 既有语义化 code → 中文(仅 auth 类少数,单独扫一遍)
    sem_re = re.compile(
        r'"code"\s*:\s*"([A-Z][A-Z0-9_]{3,})"\s*,?\s*"[^"]*"\s*:\s*"((?:[^"\\]|\\.)*[\u4e00-\u9fff](?:[^"\\]|\\.)*)"'
    )
    code_first = re.compile(
        r'"message"\s*:\s*"((?:[^"\\]|\\.)*[\u4e00-\u9fff](?:[^"\\]|\\.)*)"\s*,\s*"code"\s*:\s*"([A-Z][A-Z0-9_]{3,})"'
    )
    for path in sorted(ROOT.rglob("*.go")):
        src = path.read_text(encoding="utf-8")
        for m in sem_re.finditer(src):
            mapping.setdefault(m.group(1), m.group(2))
        for m in code_first.finditer(src):
            mapping.setdefault(m.group(2), m.group(1))

    print(f"files={stats['files']} inserted={stats['inserted']} existing_skipped={stats['existing']} unique={len(mapping)}")
    if not dry:
        out_json = Path(__file__).resolve().parent.parent / "web" / "src" / "i18n" / "api-codes.generated.json"
        out_json.parent.mkdir(parents=True, exist_ok=True)
        out_json.write_text(
            json.dumps(mapping, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
        print(f"mapping -> {out_json}")

if __name__ == "__main__":
    main()
