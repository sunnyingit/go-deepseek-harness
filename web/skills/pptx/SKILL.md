---
name: pptx
description: 做和改 PowerPoint：版式、模板、图表、图片、格式，以及批量生成幻灯片。
---

# PowerPoint skill

Build a **widescreen 16:9** deck that does not look like the Office default template.

## Hard rules

1. Import and use `scripts/pptx_helper.py` from this skill directory. Do not hand-roll `Presentation()` + `slide_layouts[0/1]`.
2. Always 13.333" × 7.5" (the helper sets this). Never 10" × 7.5" (that is 4:3).
3. Always **blank layout**. Never Title / Title and Content placeholders — leftover Calibri boxes are why decks look cheap.
4. Pick **one** theme from the helper (`slate` `night` `field` `brick` `sea`) that fits the subject. One theme for the whole file.
5. Save the `.pptx` into the **session workspace** (default cwd). The chat preview is a text dump; the real file is what matters.
6. Chinese body copy. Short headlines. Max **5** bullets per slide. Prefer a `stats` slide over a bullet list of numbers.

## How to load the helper

```python
import sys
sys.path.insert(0, "<skill-base>/scripts")  # skill-base comes from load_skill
from pptx_helper import new_deck

deck = new_deck(theme="field")  # or new_deck(subject="用户给的主题")
deck.cover("主标题", subtitle="副标题或日期", kicker="章节眉题")
deck.section("01", "这一部分在讲什么")
deck.stats("关键结果", [("128%", "同比增长"), ("36", "覆盖城市"), ("4.2", "满意度")])
deck.bullets("判断", ["第一句完整结论", "第二句", "第三句"])
deck.split("对照", ("之前", ["…"]), ("之后", ["…"]))
deck.chart("趋势", ["Q1", "Q2", "Q3", "Q4"], {"今年": [12, 15, 14, 18]}, kind="column")
deck.table("明细", ["项目", "Q1", "Q2"], [["A", "1", "2"]])
deck.quote("一句可被记住的话", "来源")
deck.close("下一步", "一句话行动")
deck.save("brief.pptx")  # workspace-relative
```

## Visual bar

- Cover: accent panel + large title. Not a lonely title floating on white.
- Content: 0.14" accent rail on the left, generous padding, no clipart, no purple gradients, no navy+gold “corporate” kit unless the brief is literally that brand.
- Type: PingFang SC / 微软雅黑 via the helper. Title ~28–36pt, body 15–18pt. Never 12pt body.
- One accent color. Cards and tables use `surface` / `bg` zebra, header row uses accent.
- If the user names a brand color, pass a custom `Theme` — do not mix it with a second palette.

## Editing an existing .pptx

You may open the file with `Presentation(path)` and change text in place. For **new slides**, still build them with this helper into a new deck and, if needed, tell the user you rebuilt the look rather than fighting the old template.

## Do not

- `pip install` (python-pptx is already in the runtime)
- Screenshot or Playwright “preview”
- Tell the user the in-chat preview is the design; tell them to download and open the `.pptx`
- Copy layouts from `examples/` — those files are API notes, not a visual standard
