---
name: d3js-visualization
description: 用 D3.js 做交互图表、仪表盘、关系图。默认写成一页离线自包含 HTML；用技能里预置的 d3.v7.min.js 内联，不要 CDN、不要 npm、不要为验收开 Chrome。
---

# D3.js Data Visualization Skill

对话页会 iframe 会话工作区里的 `.html`。演示页必须**离线也能打开**。

## 交付方式（必读，先做这个）

1. 在会话工作区写一个 HTML（例如 `funnel.html`）。页面里先放 CDN 标签当占位即可：

```html
<script src="https://d3js.org/d3.v7.min.js"></script>
```

业务脚本写在它后面。数据用 JS 数组内嵌，不要 `d3.csv("data.csv")` 去读外部文件。不要 Google Fonts。

2. 立刻用技能自带的库内联，**不要再 curl 下载 D3，不要手写 Python 拼接**：

```bash
python3 <skill-base>/scripts/inline-d3.py funnel.html
```

`vendor/d3.v7.min.js`（D3 7.9.0，约 280KB）已经在技能目录里。脚本会把 CDN `<script src>` 换成内联 `<script>`，反斜杠安全，可重复跑。

3. 文件必须在会话工作区根目录，对话里会自动出现可点预览。用简体中文告诉用户可以离线打开。

**不要：**
- `curl` / `wget` 下载 `d3.min.js`
- `npm install d3` 或 Vite/React 脚手架
- 为「不白屏」开 Chrome / Puppeteer / dump-dom（用户没要求就别验）
- 把成品只留在技能目录
- 运行时再走 CDN（老板演示常没网）

---

## What is D3.js

D3.js (Data-Driven Documents) is a JavaScript library for producing dynamic, interactive data visualizations in web browsers. It uses HTML, SVG, and CSS standards to bind data to the DOM and apply data-driven transformations.

### When to Use D3.js

**Choose D3.js when you need:**
- Custom, unique visualizations not available in chart libraries
- Fine-grained control over every visual element
- Complex interactions and animations
- Data-driven DOM manipulation beyond just charts
- Performance with large datasets (when using Canvas)
- Web standards-based visualizations

**Consider alternatives when:**
- Simple standard charts are sufficient (use Chart.js, Plotly)
- Quick prototyping is priority (use Observable, Vega-Lite)
- Static charts for print/reports (use matplotlib, ggplot2)
- 3D visualizations (use Three.js, WebGL libraries)

### D3.js vs Other Libraries

| Library | Best For | Learning Curve | Customization |
|---------|----------|----------------|---------------|
| D3.js | Custom visualizations | Steep | Complete |
| Chart.js | Standard charts | Easy | Limited |
| Plotly | Scientific plots | Medium | Good |
| Highcharts | Business dashboards | Easy | Good |
| Three.js | 3D graphics | Steep | Complete |

---

## Core Workflow

业务图直接写在第 2 步那种 `d3.select` 代码里。库用上面的 `inline-d3.py`，不要再选 CDN 或 NPM。

### Create Basic Chart

```javascript
// Set up dimensions and margins
const margin = {top: 20, right: 30, bottom: 40, left: 50};
const width = 800 - margin.left - margin.right;
const height = 400 - margin.top - margin.bottom;

// Create SVG
const svg = d3.select("#chart")
  .append("svg")
  .attr("width", width + margin.left + margin.right)
  .attr("height", height + margin.top + margin.bottom)
  .append("g")
  .attr("transform", `translate(${margin.left},${margin.top})`);

// Load and process data
d3.csv("data.csv", d => ({
  date: new Date(d.date),
  value: +d.value
})).then(data => {

  // Create scales
  const xScale = d3.scaleTime()
    .domain(d3.extent(data, d => d.date))
    .range([0, width]);

  const yScale = d3.scaleLinear()
    .domain([0, d3.max(data, d => d.value)])
    .nice()
    .range([height, 0]);

  // Create and append axes
  svg.append("g")
    .attr("transform", `translate(0,${height})`)
    .call(d3.axisBottom(xScale));

  svg.append("g")
    .call(d3.axisLeft(yScale));

  // Create line generator
  const line = d3.line()
    .x(d => xScale(d.date))
    .y(d => yScale(d.value))
    .curve(d3.curveMonotoneX);

  // Draw line
  svg.append("path")
    .datum(data)
    .attr("d", line)
    .attr("fill", "none")
    .attr("stroke", "steelblue")
    .attr("stroke-width", 2);
});
```

### 3. Add Interactivity

**Tooltips:**

```javascript
const tooltip = d3.select("body")
  .append("div")
  .attr("class", "tooltip")
  .style("position", "absolute")
  .style("visibility", "hidden")
  .style("background", "white")
  .style("border", "1px solid #ddd")
  .style("padding", "10px")
  .style("border-radius", "4px");

circles
  .on("mouseover", function(event, d) {
    tooltip
      .style("visibility", "visible")
      .html(`<strong>${d.name}</strong><br/>Value: ${d.value}`);
  })
  .on("mousemove", function(event) {
    tooltip
      .style("top", (event.pageY - 10) + "px")
      .style("left", (event.pageX + 10) + "px");
  })
  .on("mouseout", function() {
    tooltip.style("visibility", "hidden");
  });
```

**Transitions:**

```javascript
circles
  .transition()
  .duration(300)
  .ease(d3.easeCubicOut)
  .attr("r", 8);
```

### 4. Implement Responsive Design

```javascript
function createChart() {
  const container = d3.select("#chart");
  const containerWidth = container.node().getBoundingClientRect().width;

  const margin = {top: 20, right: 30, bottom: 40, left: 50};
  const width = containerWidth - margin.left - margin.right;
  const height = Math.min(width * 0.6, 500);

  container.selectAll("*").remove(); // Clear previous

  // Create SVG...
}

// Initial render
createChart();

// Re-render on resize with debouncing
let resizeTimer;
window.addEventListener("resize", () => {
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(createChart, 250);
});
```

---

## Key Principles

### Data Binding
- Use `.data()` to bind data to DOM elements
- Handle enter, update, and exit selections
- Use key functions for consistent element-to-data matching
- Modern syntax: use `.join()` for cleaner code

### Scales
- Map data values (domain) to visual values (range)
- Use appropriate scale types (linear, time, band, ordinal)
- Apply `.nice()` to scales for rounded axis values
- Invert y-scale range for bottom-up coordinates: `[height, 0]`

### SVG Coordinate System
- Origin (0,0) is at top-left corner
- Y increases downward (opposite of Cartesian)
- Use margin convention for proper spacing
- Group related elements with `<g>` tags

### Performance
- Use SVG for <1,000 elements
- Use Canvas for >1,000 elements
- Aggregate or sample large datasets
- Debounce resize handlers

---

## Chart Selection Guide

**Time series data?** → Line chart or area chart

**Comparing categories?** → Bar chart (vertical or horizontal)

**Showing relationships?** → Scatter plot or bubble chart

**Part-to-whole?** → Donut chart or stacked bar (limit to 5-7 categories)

**Network data?** → Force-directed graph

**Distribution?** → Histogram or box plot

See [`references/chart-types.md`](./references/chart-types.md) for detailed chart selection criteria and best practices.

---

## Common Patterns

### Quick Data Loading

```javascript
// Load CSV with type conversion
d3.csv("data.csv", d => ({
  date: new Date(d.date),
  value: +d.value,
  category: d.category
})).then(data => {
  createChart(data);
});
```

### Quick Tooltip

```javascript
selection
  .on("mouseover", (event, d) => {
    tooltip.style("visibility", "visible").html(`Value: ${d.value}`);
  })
  .on("mousemove", (event) => {
    tooltip.style("top", event.pageY + "px").style("left", event.pageX + "px");
  })
  .on("mouseout", () => tooltip.style("visibility", "hidden"));
```

### Quick Responsive SVG

```javascript
svg
  .attr("viewBox", `0 0 ${width} ${height}`)
  .attr("preserveAspectRatio", "xMidYMid meet")
  .style("width", "100%")
  .style("height", "auto");
```

---

## Quality Standards

### Visual Quality
- Use appropriate chart type for data
- Apply consistent color schemes
- Include clear axis labels and legends
- Provide proper spacing with margin convention
- Use appropriate scale types and ranges

### Interaction Quality
- Add meaningful tooltips
- Use smooth transitions (300-500ms duration)
- Provide hover feedback
- Enable keyboard navigation for accessibility
- Implement zoom/pan for detailed exploration

### Code Quality
- Use key functions in data joins
- Handle enter, update, and exit properly
- Clean up previous renders before updates
- Use reusable chart pattern for modularity
- Debounce expensive operations

### Accessibility
- Add ARIA labels and descriptions
- Provide keyboard navigation
- Use colorblind-safe palettes
- Include text alternatives for screen readers
- Ensure sufficient color contrast

---

## Helper Resources

### Available Scripts
- **data-helpers.js**: Data loading, parsing, and transformation utilities
- **chart-templates.js**: Reusable chart templates for common visualizations

See [`scripts/`](./scripts/) directory for implementations.

### Working Examples
- **line-chart.html**: Time series visualization with tooltips
- **bar-chart.html**: Grouped and stacked bar charts
- **network-graph.html**: Force-directed network visualization

See [`examples/`](./examples/) directory for complete implementations.

### Detailed References
- **D3 Fundamentals**: SVG basics, data binding, selections, transitions, events
  → [`references/d3-fundamentals.md`](./references/d3-fundamentals.md)

- **Scales and Axes**: All scale types, axis customization, color palettes
  → [`references/scales-and-axes.md`](./references/scales-and-axes.md)

- **Paths and Shapes**: Line/area generators, arcs, force simulations
  → [`references/paths-and-shapes.md`](./references/paths-and-shapes.md)

- **Data Transformation**: Loading, parsing, grouping, aggregation, date handling
  → [`references/data-transformation.md`](./references/data-transformation.md)

- **Chart Types**: Detailed guidance on when to use each chart type
  → [`references/chart-types.md`](./references/chart-types.md)

- **Advanced Patterns**: Reusable charts, performance optimization, responsive design
  → [`references/advanced-patterns.md`](./references/advanced-patterns.md)

- **Common Pitfalls**: Frequent mistakes and their solutions
  → [`references/common-pitfalls.md`](./references/common-pitfalls.md)

- **Integration Patterns**: Using D3 with React, Vue, Angular, Svelte
  → [`references/integration-patterns.md`](./references/integration-patterns.md)

---

## Troubleshooting

**Chart not appearing?**
- Check browser console for errors
- Verify data loaded correctly
- Ensure SVG has width and height
- Check scale domains and ranges

**Elements in wrong position?**
- Verify scale domain matches data range
- Check if y-scale range is inverted: `[height, 0]`
- Confirm margin transform applied to `<g>` element
- Check SVG coordinate system (top-left origin)

**Transitions not working?**
- Ensure duration is reasonable (300-500ms)
- Check if transition applied to selection, not data
- Verify easing function is valid
- Confirm elements exist before transitioning

**Poor performance?**
- Reduce number of DOM elements (use Canvas if >1,000)
- Aggregate or sample data
- Debounce resize handlers
- Minimize redraws

---

## External Resources

### Official Documentation
- D3.js API Reference: https://d3js.org/
- Observable Examples: https://observablehq.com/@d3

### Learning Resources
- "Interactive Data Visualization for the Web" by Scott Murray
- D3 Graph Gallery: https://d3-graph-gallery.com/
- Amelia Wattenberger's D3 Tutorial: https://wattenberger.com/blog/d3

### Color Tools
- ColorBrewer: https://colorbrewer2.org/
- D3 Color Schemes: https://d3js.org/d3-scale-chromatic

### Inspiration
- Observable Trending: https://observablehq.com/trending
- Reddit r/dataisbeautiful: https://reddit.com/r/dataisbeautiful

---

This skill provides comprehensive coverage of D3.js for creating professional, interactive data visualizations. Use the core workflow as a starting point, refer to the detailed references for specific topics, and customize the examples for your needs.
