#!/usr/bin/env python3
"""Собирает design/tokens.json из apps/ios/CBRPlayer/Helpers/Theme.swift.

Палитра — все `static let <шкала><шаг> = Color(hex: 0x…)` из Palette, роли — `static let <роль>`
из Theme с комментарием `///` над ними. Запускать из корня репозитория.
"""
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SRC = ROOT / "apps/ios/CBRPlayer/Helpers/Theme.swift"
OUT = ROOT / "design/tokens.json"

src = SRC.read_text()
palette: dict[str, dict[str, str]] = {}
for m in re.finditer(r"static let (\w+?)(\d+) = Color\(hex: 0x([0-9A-F]{6})\)", src):
    palette.setdefault(m.group(1), {})[m.group(2)] = "#" + m.group(3)

roles = {}
theme = src[src.index("enum Theme"):]
for m in re.finditer(r"/// (.*?)\n\s*static let (\w+) = (?:Palette\.(\w+?)(\d+)|Color\.white)", theme):
    desc, name, fam, step = m.groups()
    roles[name] = {
        "ref": f"{fam}.{step}" if fam else "white",
        "hex": palette[fam][step] if fam else "#FFFFFF",
        "use": desc,
    }

tokens = {
    "$comment": "Сгенерировано из apps/ios/CBRPlayer/Helpers/Theme.swift (design/README.md). "
                "Palette — тональные шкалы 50…950 в OKLCH; roles — назначение цветов в интерфейсе.",
    "palette": palette,
    "roles": roles,
    "radius": {"tile": 20, "row": 12, "field": 6, "button": 10},
    "spacing": {"tileGap": 24, "tilePadding": 24, "tileMinHeight": 232, "contentMaxWidth": 960, "screenPadding": 48},
}
OUT.write_text(json.dumps(tokens, ensure_ascii=False, indent=2) + "\n")
print(f"{OUT.relative_to(ROOT)}: {len(palette)} шкал, {len(roles)} ролей")
