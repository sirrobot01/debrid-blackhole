package config

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (c *Config) IsAllowedFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return false
	}
	// Remove the leading dot
	ext = ext[1:]

	for _, allowed := range c.AllowedExt {
		if ext == allowed {
			return true
		}
	}
	return false
}

func getDefaultExtensions() []string {
	videoExts := strings.Split("YUV,WMV,WEBM,VOB,VIV,SVI,ROQ,RMVB,RM,OGV,OGG,NSV,MXF,MPG,MPEG,M2V,MP2,MPE,MPV,MP4,M4P,M4V,MOV,QT,MNG,MKV,FLV,DRC,AVI,ASF,AMV,MKA,F4V,3GP,3G2,DIVX,X264,X265", ",")
	musicExts := strings.Split("MP3,WAV,FLAC,OGG,WMA,AIFF,ALAC,M4A,APE,AC3,DTS,M4P,MID,MIDI,MKA,MP2,MPA,RA,VOC,WV,AMR", ",")

	// Combine both slices
	allExts := append(videoExts, musicExts...)

	// Convert to lowercase
	for i, ext := range allExts {
		allExts[i] = strings.ToLower(ext)
	}

	// Remove duplicates
	seen := make(map[string]bool)
	var unique []string

	for _, ext := range allExts {
		if !seen[ext] {
			seen[ext] = true
			unique = append(unique, ext)
		}
	}

	sort.Strings(unique)
	return unique
}

func parseSize(sizeStr string) (int64, error) {
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))

	// Absolute size-based cache
	multiplier := 1.0
	if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "GB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "KB")
	}

	size, err := strconv.ParseFloat(sizeStr, 64)
	if err != nil {
		return 0, err
	}

	return int64(size * multiplier), nil
}
