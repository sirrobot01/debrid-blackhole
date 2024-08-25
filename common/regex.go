package common

import (
	"regexp"
)

var (
	VIDEOMATCH = "(?i)(\\.)(YUV|WMV|WEBM|VOB|VIV|SVI|ROQ|RMVB|RM|OGV|OGG|NSV|MXF|MTS|M2TS|TS|MPG|MPEG|M2V|MP2|MPE|MPV|MP4|M4P|M4V|MOV|QT|MNG|MKV|FLV|DRC|AVI|ASF|AMV)$"
	SUBMATCH   = "(?i)(\\.)(SRT|SUB|SBV|ASS|VTT|TTML|DFXP|STL|SCC|CAP|SMI|TTXT|TDS|USF|JSS|SSA|PSB|RT|LRC|SSB)$"
)

func RegexMatch(regex string, value string) bool {
	re := regexp.MustCompile(regex)
	return re.MatchString(value)
}

func RemoveExtension(value string) string {
	re := regexp.MustCompile(VIDEOMATCH)

	// Find the last index of the matched extension
	loc := re.FindStringIndex(value)
	if loc != nil {
		return value[:loc[0]]
	} else {
		return value
	}
}
