package server

import "regexp"

var argIDPattern = regexp.MustCompile(`\b(bull|bear)\.(\d+)\b`)

// speakArgIDs rewrites the argument ids a model sometimes leaves in its prose (bull.1, bear.2) into words
// the reader understands. The prompt already asks the chief not to write them; this is the net under it.
// Only prose goes through here: the adopted and rejected id fields keep the real ids.
func speakArgIDs(text string) string {
	return argIDPattern.ReplaceAllStringFunc(text, func(id string) string {
		m := argIDPattern.FindStringSubmatch(id)
		who := "看多分析师"
		if m[1] == "bear" {
			who = "看空分析师"
		}
		return who + "的第 " + m[2] + " 条"
	})
}
