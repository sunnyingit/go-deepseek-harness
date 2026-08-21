---
name: sales-report
description: 用自带脚本生成一份示例销售报表 CSV。
---
Run the bundled script with the bash tool: command="python3 <base directory>/scripts/generate.py",
using the base directory named in this skill's resource hint above, and leave workdir unset
so the script's relative output path resolves into your workspace. Do not reimplement the
script inline — it is the source of truth for the report's exact columns and formatting.
