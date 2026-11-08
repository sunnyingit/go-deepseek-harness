package inbox

import (
	"fmt"
	"strings"
	"time"
)

// Two notices from the quality screen: a stock the user holds or wants to buy that has stepped on a red line, and
// the better-quality companies in the same industry as a stock the user wants to buy.
func init() {
	Registry["quality.redline"] = Type{TTL: 7 * 24 * time.Hour, Severity: "normal", Tone: "warn", Group: "stock", Link: stockLink("holdings", "chk"),
		Render: func(p map[string]any) (string, string) {
			flags := list(p, "flags")
			title := fmt.Sprintf("%s 踩到质地红线", str(p, "name"))
			if len(flags) > 0 {
				title = fmt.Sprintf("%s（你%s）踩到质地红线：%s", str(p, "name"), str(p, "kind_text"), flags[0])
			}
			return title, strings.Join(flags, "\n")
		}}
	Registry["quality.picks"] = Type{TTL: 7 * 24 * time.Hour, Severity: "info", Tone: "blue", Group: "stock", Link: stockLink("holdings", "chk"),
		Render: func(p map[string]any) (string, string) {
			title := fmt.Sprintf("你想买的 %s：同赛道质地靠前的公司", str(p, "name"))
			body := strings.Join(list(p, "items"), "\n")
			return title, body + "\n按质地规则筛出来的，不是买入建议：好公司也可能贵，也可能在弱势板块里。"
		}}
}
