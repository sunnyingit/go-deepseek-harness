---
name: xlsx
description: 处理 Excel / CSV：建表、公式、格式、图表、分析和改已有表格。透视汇总用 pandas.pivot_table，禁止用 openpyxl.pivot XML。
---

# Excel (XLSX) Skill

## Hard rules（先读，再写脚本）

Runtime 已预装 **openpyxl** 和 **pandas**。不要 `pip install`、不要翻源码、不要试探 API。

**透视表 / Pivot Table 的唯一做法：** 用 `pandas.pivot_table` 或 `groupby` 算出汇总，写成「透视汇总」工作表（可加合计行、数字格式、图表）。源数据做成 Excel 表（`openpyxl.worksheet.table.Table`），方便用户之后在 Excel 里插入真正的数据透视表。

**禁止（会空转十几轮、文件还可能坏）：**

- `from openpyxl.pivot...`、`CacheDefinition`、`TableDefinition`、`PivotField`、`CacheSource`、`ws.add_pivot`、`ws._pivots`
- inspect / 打印 `__init__` / 读 site-packages 里的 openpyxl 源码来「拼 OOXML 透视表」
- 为了「必须是 Excel 原生透视表」而反复改脚本

openpyxl 3.1 读透视表可以，**写不出可靠的 Excel 原生 PivotTable**。用户说「透视出来」= 交付带明细 + 透视汇总 + 图表的 xlsx，并在汇总表注明按分类透视。一次写完、跑通、保存。

## Overview

openpyxl 负责读写与格式/图表/公式；pandas 负责分析和透视汇总。

## Core Capabilities

### 1. File Operations
- **Reading**: Load .xlsx, .xlsm, and .csv files
- **Writing**: Create new Excel workbooks from scratch
- **Editing**: Modify existing workbooks while preserving formulas, formatting, and charts
- **Converting**: Transform between CSV, Excel, and other formats

### 2. Data Management
- **Cell Operations**: Read, write, and modify individual cells or ranges
- **Formulas**: Create and manage Excel formulas (SUM, VLOOKUP, INDEX/MATCH, etc.)
- **Data Validation**: Set dropdown lists, numeric ranges, date constraints
- **Named Ranges**: Define and use named cell ranges for easier formula management

### 3. Formatting
- **Cell Styling**: Fonts, colors, borders, alignment, number formats
- **Conditional Formatting**: Apply rules-based formatting
- **Row/Column Sizing**: Set widths, heights, auto-fit
- **Merge Cells**: Combine cells for headers and labels

### 4. Charts & Visualizations
- **Chart Types**: Line, bar, column, pie, scatter, area, combo charts
- **Chart Customization**: Titles, legends, data labels, colors
- **Multiple Series**: Multi-dataset charts with secondary axes
- **Chart Positioning**: Place charts in specific locations

### 5. Multi-Worksheet Operations
- **Sheet Management**: Create, rename, delete, reorder worksheets
- **Cross-Sheet Formulas**: Reference data across multiple sheets
- **Sheet Copying**: Duplicate sheets with formatting intact
- **Sheet Protection**: Lock/unlock sheets and ranges

### 6. Data Analysis
- **Filtering**: Auto-filter data ranges
- **Sorting**: Multi-level sorting
- **Pivot summaries**: `pandas.pivot_table` written as a formatted sheet (not Excel PivotTable XML)
- **Statistical Functions**: Built-in and custom calculations

## Libraries

Already available. Import and go:

```python
import pandas as pd
from openpyxl import Workbook, load_workbook
from openpyxl.styles import Font, PatternFill, Alignment, Border, Side, numbers
from openpyxl.chart import BarChart, PieChart, Reference
from openpyxl.utils import get_column_letter
from openpyxl.worksheet.table import Table, TableStyleInfo
```

## Workflow: 透视汇总（用户要「透视表」时照抄）

```python
import pandas as pd
from openpyxl import Workbook
from openpyxl.chart import BarChart, Reference
from openpyxl.styles import Font, PatternFill, Alignment, numbers
from openpyxl.utils import get_column_letter
from openpyxl.worksheet.table import Table, TableStyleInfo

df = pd.DataFrame(rows)  # 至少含分类列 + 金额列
pivot = pd.pivot_table(
    df, values="金额", index="分类", columns=None,
    aggfunc="sum", fill_value=0, margins=True, margins_name="合计",
)

wb = Workbook()
ws = wb.active
ws.title = "明细"
ws.append(list(df.columns))
for row in df.itertuples(index=False):
    ws.append(list(row))
last = ws.max_row
tbl = Table(displayName="明细表", ref=f"A1:{get_column_letter(ws.max_column)}{last}")
tbl.tableStyleInfo = TableStyleInfo(name="TableStyleMedium2", showRowStripes=True)
ws.add_table(tbl)

ws_p = wb.create_sheet("透视汇总")
ws_p["A1"] = "透视汇总（pandas 按分类求和；可在 Excel 中对「明细」再插入数据透视表）"
ws_p["A1"].font = Font(bold=True, size=14)
flat = pivot.reset_index()
ws_p.append(list(flat.columns))
for row in flat.itertuples(index=False):
    ws_p.append(list(row))
for cell in ws_p[2]:
    cell.fill = PatternFill("solid", fgColor="217346")
    cell.font = Font(color="FFFFFF", bold=True)
    cell.alignment = Alignment(horizontal="center")
for row in ws_p.iter_rows(min_row=3, min_col=2, max_col=ws_p.max_column):
    for cell in row:
        if isinstance(cell.value, (int, float)):
            cell.number_format = '#,##0.00'

chart = BarChart()
chart.title = "分类合计"
chart.add_data(Reference(ws_p, min_col=2, min_row=2, max_row=ws_p.max_row - 1), titles_from_data=True)
chart.set_categories(Reference(ws_p, min_col=1, min_row=3, max_row=ws_p.max_row - 1))
ws_p.add_chart(chart, "E2")
wb.save("report.xlsx")
```

## Essential Workflows

### Workflow 1: Creating a New Workbook from Scratch

```python
from openpyxl import Workbook
from openpyxl.styles import Font, PatternFill, Alignment
from openpyxl.utils import get_column_letter

# Create new workbook
wb = Workbook()
ws = wb.active
ws.title = "Sales Report"

# Add headers with formatting
headers = ["Product", "Q1", "Q2", "Q3", "Q4", "Total"]
header_fill = PatternFill(start_color="4472C4", end_color="4472C4", fill_type="solid")
header_font = Font(color="FFFFFF", bold=True)

for col, header in enumerate(headers, start=1):
    cell = ws.cell(row=1, column=col, value=header)
    cell.fill = header_fill
    cell.font = header_font
    cell.alignment = Alignment(horizontal="center")

# Add data
data = [
    ["Product A", 1000, 1200, 1100, 1300],
    ["Product B", 800, 900, 950, 1000],
    ["Product C", 1500, 1400, 1600, 1700]
]

for row_idx, row_data in enumerate(data, start=2):
    for col_idx, value in enumerate(row_data, start=1):
        ws.cell(row=row_idx, column=col_idx, value=value)

# Add formulas for totals
for row in range(2, len(data) + 2):
    formula = f"=SUM(B{row}:E{row})"
    ws.cell(row=row, column=6, value=formula)

# Adjust column widths
for col in range(1, 7):
    ws.column_dimensions[get_column_letter(col)].width = 12

# Save workbook
wb.save("sales_report.xlsx")
```

### Workflow 2: Reading and Analyzing Existing Workbooks

```python
from openpyxl import load_workbook

# Load existing workbook
wb = load_workbook('data.xlsx', data_only=True)  # data_only=True evaluates formulas
ws = wb.active

# Method 1: Iterate through all rows
for row in ws.iter_rows(min_row=2, values_only=True):
    print(row)

# Method 2: Read specific cells
value = ws['A1'].value
value = ws.cell(row=2, column=2).value

# Method 3: Read range
for row in ws['B2':'D5']:
    for cell in row:
        print(cell.value, end=' ')
    print()

# Calculate statistics
values = [cell.value for cell in ws['B'][1:] if isinstance(cell.value, (int, float))]
if values:
    print(f"Sum: {sum(values)}")
    print(f"Average: {sum(values) / len(values):.2f}")

wb.close()
```

### Workflow 3: Editing Workbooks While Preserving Formulas

```python
from openpyxl import load_workbook
from openpyxl.styles import Font

# Load workbook WITHOUT data_only to preserve formulas
wb = load_workbook('existing_report.xlsx')
ws = wb['Sales']

# Update values (formulas will recalculate when opened in Excel)
ws['B2'] = 1500
ws['C2'] = 1650

# Add new row with data and formulas
new_row = ws.max_row + 1
ws[f'A{new_row}'] = "Product D"
ws[f'B{new_row}'] = 900
ws[f'C{new_row}'] = 1000
ws[f'D{new_row}'] = 1100
ws[f'E{new_row}'] = 1200
ws[f'F{new_row}'] = f"=SUM(B{new_row}:E{new_row})"  # Add formula

# Apply formatting to new row
for col in range(1, 7):
    cell = ws.cell(row=new_row, column=col)
    if col == 1:
        cell.font = Font(bold=True)

# Save changes
wb.save('existing_report.xlsx')
```

### Workflow 4: Working with Pandas for Data Analysis

```python
import pandas as pd
from openpyxl import load_workbook
from openpyxl.styles import Font, PatternFill

# Step 1: Read and analyze data with pandas
df = pd.read_excel('sales_data.xlsx')

# Perform analysis
summary = df.groupby('Product').agg({
    'Sales': ['sum', 'mean', 'count'],
    'Profit': 'sum'
}).round(2)

summary.columns = ['Total Sales', 'Avg Sales', 'Transactions', 'Total Profit']

# Step 2: Write results to new Excel file
with pd.ExcelWriter('sales_analysis.xlsx', engine='openpyxl') as writer:
    df.to_excel(writer, sheet_name='Raw Data', index=False)
    summary.to_excel(writer, sheet_name='Summary')

# Step 3: Enhance with openpyxl formatting
wb = load_workbook('sales_analysis.xlsx')
ws = wb['Summary']

header_fill = PatternFill(start_color="366092", end_color="366092", fill_type="solid")
header_font = Font(color="FFFFFF", bold=True)

for cell in ws[1]:
    cell.fill = header_fill
    cell.font = header_font

wb.save('sales_analysis.xlsx')
```

## Key Principles

### Formula Management
- **Always use formulas for calculations** instead of hardcoded values
- Formulas update automatically when source data changes
- Use named ranges for complex formulas to improve readability
- Don't use `data_only=True` when loading files if you need to preserve formulas

### Performance Optimization
- **For large datasets**: Write rows in bulk using `ws.append()`
- **For reading large files**: Use `read_only=True` mode
- **For writing large files**: Use `write_only=True` mode
- Avoid cell-by-cell operations in nested loops

### Memory Management
- Close workbooks after use: `wb.close()`
- Use read_only/write_only modes for large files
- Process data in chunks for very large datasets

### Error Handling
- Always use try/except blocks for file operations
- Check for empty cells before processing
- Validate data types before calculations
- Handle InvalidFileException for corrupted files

### Date and Time
- Use `datetime` objects for dates, not strings
- Apply proper number formats: `cell.number_format = 'mm/dd/yyyy'`
- Excel stores dates as numbers internally

## Quick Reference

### Basic Operations
```python
from openpyxl import Workbook, load_workbook

# Create workbook
wb = Workbook()
ws = wb.active

# Read cell
value = ws['A1'].value
value = ws.cell(row=1, column=1).value

# Write cell
ws['A1'] = "Hello"
ws.cell(row=1, column=1, value="Hello")

# Write formula
ws['C1'] = "=A1+B1"

# Add row
ws.append([1, 2, 3])

# Save and close
wb.save('output.xlsx')
wb.close()
```

### Common Imports
```python
from openpyxl import Workbook, load_workbook
from openpyxl.styles import Font, PatternFill, Border, Side, Alignment
from openpyxl.chart import LineChart, BarChart, PieChart, Reference
from openpyxl.utils import get_column_letter
from openpyxl.data_validation import DataValidation
from openpyxl.formatting.rule import ColorScaleRule, CellIsRule
```

## Common Use Cases

### Creating Charts
Add visualizations to your spreadsheets. See `examples/workflow-examples.md` for complete chart creation workflow including line charts, bar charts, and pie charts.

### Conditional Formatting
Apply visual formatting based on cell values. See `examples/workflow-examples.md` for color scales, icon sets, and rule-based formatting.

### Data Validation
Create dropdown lists and input constraints. See `examples/workflow-examples.md` for dropdown lists, numeric ranges, and date validation.

### Multi-Sheet Workbooks
Work with multiple worksheets and cross-sheet formulas. See `examples/workflow-examples.md` for complete multi-sheet workflow.

### Financial Reports
Create professional financial statements. See `examples/financial-report.md` for a complete income statement example with dynamic formulas.

### Data Transformation
Transform CSV data into formatted Excel reports. See `examples/data-transformation.md` for pandas integration and pivot table creation.

### Dashboards
Build executive dashboards with multiple charts. See `examples/dashboard-creation.md` for comprehensive dashboard with KPIs and visualizations.

## Helper Scripts

The `scripts/` directory provides utility functions for common operations:

```python
from scripts.excel_helper import (
    create_workbook,
    read_excel_data,
    add_chart,
    apply_formatting,
    add_formula,
    auto_fit_columns,
    add_pivot_sheet,
    add_excel_table,
)

# Create new workbook with data
wb, ws = create_workbook("Sales Report", headers=["Product", "Q1", "Q2"])

# Read data from existing file
data = read_excel_data("data.xlsx", sheet_name="Sheet1")

# Add chart to worksheet
add_chart(ws, chart_type="line", data_range="B2:D10", title="Sales Trend")

# Apply formatting
apply_formatting(ws, cell_range="A1:D1", bold=True, bg_color="4472C4")

# Add formula to range
add_formula(ws, cell="E2", formula="=SUM(B2:D2)", copy_down=10)

# Auto-fit all columns
auto_fit_columns(ws)

wb.save("output.xlsx")
```

## Additional Resources

### Detailed Documentation
- **Library Reference**: See `references/library-reference.md` for complete openpyxl, pandas, and xlsxwriter documentation
- **Best Practices**: See `references/best-practices.md` for performance optimization, error handling, and common pitfalls

### Complete Examples
- **Workflow Examples**: `examples/workflow-examples.md` - Charts, conditional formatting, data validation, multi-sheet operations
- **Financial Reports**: `examples/financial-report.md` - Income statement with dynamic formulas
- **Data Transformation**: `examples/data-transformation.md` - CSV to Excel with pandas integration
- **Dashboard Creation**: `examples/dashboard-creation.md` - Multi-chart dashboard with KPIs

### External Links
- **openpyxl Documentation**: https://openpyxl.readthedocs.io/
- **pandas Excel Support**: https://pandas.pydata.org/docs/reference/io.html#excel
- **Excel Formula Reference**: https://support.microsoft.com/en-us/excel

## Summary

This skill enables comprehensive Excel automation including:
- Creating complex spreadsheets with formulas and formatting
- Reading and analyzing existing workbooks
- Editing files while preserving formulas and styles
- Creating professional charts and visualizations
- Applying conditional formatting and data validation
- Working with multiple worksheets and cross-sheet formulas
- Integrating with pandas for advanced data analysis
- Handling large datasets efficiently

Use this skill for any task involving Excel files, from simple data entry to complex financial reports and dashboards.
