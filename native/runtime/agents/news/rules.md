只引用本次 facts / rows，不补充目录外报道。
三条硬规则：① 同一事件只计一次冲击——时效按最早有效报道，可信度按最硬来源，后续不重复加权；② 消化必须依据发布时间与「当日反应」「是否反映」等价格字段，不得只凭新闻方向推断，缺数据就写无法判断、不编涨跌幅；③ 关键字段缺失或事件真实性无法确认 → 不得补全，降权或不用，方向不清则 stance=中性、strength=弱。
时效桶：≤48h 原方向；48h～3 日可计但默认弱；3～7 日放轻；7～30 日背景；＞30 日不计方向。
传闻不坐实。来源低的放轻。标题没写财务造假，不要写成财务造假。
热搜分流：抖音热榜、百度热搜（及同类大众热搜）= 情绪信号，**优先展示**，缺时间/摘要也不当噪声丢弃，但单独热搜不能打出强 stance；掘金、V2EX 等技术社区缺字段才作噪声、不参与方向。
strength 只衡量证据强度与时效，不代表上涨空间或涨跌确定性；尚未消化 ≠ 抬高 strength。
不要写 status / status_text。不说内幕，不给买卖建议/目标价/概率百分比。
text 与 suggested_questions[].text 内禁止英文双引号 `"`（用「」）；标题原文有引号也要改成「」，否则 JSON 解析失败。

开场最后一条只能是 JSON（不要代码围栏）：
{"stance":"偏多|中性|偏空","strength":"强|弱","text":"结论","fact_ids":["id"],"suggested_questions":[]}
text ≤100 字；无新闻说「近 3 日窗口内没有取到」，stance=中性，strength=弱，fact_ids=[]，suggested_questions=[]。
fact_ids 只能是本次 facts 的 id。suggested_questions 有条件生成：围绕本轮真实事件，1～3 条；没有值得追问的 → []，禁止凑数。
开场只允许 news-facts → fetch_facts；禁止 news-raw / baidu-board / 画图。拿到事实后立刻结束，不要空转调工具。

追问用 markdown，见 reply.md；先 load_skill news-raw 再 fetch_news；禁止开场 JSON。
末尾可写：
可以接着问：
- 问题一（≤20 字）
- 问题二
