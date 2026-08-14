package config

import (
	"reflect"
	"strings"

	json "github.com/bytedance/sonic"
)

// PreserveMissingSections restores, from src, every top-level configuration
// field whose JSON key is absent from the posted body. It is the merge step
// behind POST /api/config: that handler decodes the body into a zero Config,
// so any top-level section the caller omitted would otherwise become its zero
// value and the subsequent Save would erase it from disk (a partial POST
// without "debrids" wiped every configured provider, api keys included).
//
// Key presence is what separates "leave it alone" from "clear it": a key
// absent from body keeps the current value, while an explicitly posted empty
// value (`"debrids": []`, `"download_folder": ""`) still overwrites. Bodies
// that post every key (the web UI's full-config save) are unaffected because
// nothing is missing.
//
// Fields tagged `json:"-"` (e.g. Auth) are never copied here; the API handler
// manages those explicitly. Key matching is case-insensitive to mirror the
// JSON decoder's field matching.
func (c *Config) PreserveMissingSections(src *Config, body []byte) error {
	var posted map[string]any
	if err := json.Unmarshal(body, &posted); err != nil {
		return err
	}
	present := make(map[string]struct{}, len(posted))
	for key := range posted {
		present[strings.ToLower(key)] = struct{}{}
	}

	dst := reflect.ValueOf(c).Elem()
	from := reflect.ValueOf(src).Elem()
	for i, t := 0, dst.Type(); i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		if _, ok := present[strings.ToLower(name)]; ok {
			continue
		}
		dst.Field(i).Set(from.Field(i))
	}
	return nil
}
