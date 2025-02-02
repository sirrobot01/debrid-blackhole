package utils

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	VIDEOMATCH = "(?i)(\\.)(YUV|WMV|WEBM|VOB|VIV|SVI|ROQ|RMVB|RM|OGV|OGG|NSV|MXF|MPG|MPEG|M2V|MP2|MPE|MPV|MP4|M4P|M4V|MOV|QT|MNG|MKV|FLV|DRC|AVI|ASF|AMV|MKA|F4V|3GP|3G2|DIVX|X264|X265)$"
	MUSICMATCH = "(?i)(\\.)(?:MP3|WAV|FLAC|AAC|OGG|WMA|AIFF|ALAC|M4A|APE|AC3|DTS|M4P|MID|MIDI|MKA|MP2|MPA|RA|VOC|WV|AMR)$"
)

var SAMPLEMATCH = `(?i)(^|[\\/]|[._-])(sample|trailer|thumb)s?([._-]|$)`

func RegexMatch(regex string, value string) bool {
	re := regexp.MustCompile(regex)
	return re.MatchString(value)
}

func RemoveInvalidChars(value string) string {
	return strings.Map(func(r rune) rune {
		if r == filepath.Separator || r == ':' {
			return r
		}
		if filepath.IsAbs(string(r)) {
			return r
		}
		if strings.ContainsRune(filepath.VolumeName("C:"+string(r)), r) {
			return r
		}
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, value)
}

func RemoveExtension(value string) string {
	re := regexp.MustCompile(VIDEOMATCH + "|" + SAMPLEMATCH + "|" + MUSICMATCH)

	// Find the last index of the matched extension
	loc := re.FindStringIndex(value)
	if loc != nil {
		return value[:loc[0]]
	} else {
		return value
	}
}
