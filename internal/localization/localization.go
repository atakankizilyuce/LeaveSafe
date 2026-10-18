// Package localization keeps terminal copy separate from protocol and log identifiers.
package localization

import (
	"embed"
	"encoding/json"
	"os"
	"strings"
)

//go:embed catalogs/*.json service/*.json
var files embed.FS

// Language labels are written in their own language so the choice remains readable.
type Language struct{ Code, Name string }

var Languages = []Language{
	{"tr", "Türkçe"}, {"en", "English"}, {"zh-Hans", "简体中文"}, {"hi", "हिन्दी"},
	{"es", "Español"}, {"ar", "العربية"}, {"fr", "Français"}, {"bn", "বাংলা"},
	{"pt", "Português"}, {"id", "Bahasa Indonesia"}, {"ur", "اردو"}, {"ru", "Русский"},
	{"de", "Deutsch"}, {"ja", "日本語"}, {"pcm", "Naijá"}, {"arz", "مصري"},
	{"mr", "मराठी"}, {"vi", "Tiếng Việt"}, {"te", "తెలుగు"}, {"sw", "Kiswahili"},
	{"ha", "Hausa"}, {"pnb-Arab", "پنجابی"}, {"tl", "Filipino"}, {"ta", "தமிழ்"}, {"yue-Hant", "粵語"},
}
var catalogs = loadCatalogs()

func loadCatalogs() map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, language := range Languages {
		messages := make(map[string]string)
		for _, directory := range []string{"catalogs", "service"} {
			data, err := files.ReadFile(directory + "/" + language.Code + ".json")
			if err != nil {
				continue
			}
			var section map[string]string
			if json.Unmarshal(data, &section) != nil {
				continue
			}
			for key, value := range section {
				messages[key] = value
			}
		}
		if len(messages) > 0 {
			result[language.Code] = messages
		}
	}
	return result
}

// Normalize accepts stored tags and operating-system locale names without guessing a script.
func Normalize(value string) string {
	tag := strings.ReplaceAll(strings.Split(strings.Split(value, ".")[0], "@")[0], "_", "-")
	for _, language := range Languages {
		if strings.EqualFold(language.Code, tag) {
			return language.Code
		}
	}
	lower := strings.ToLower(tag)
	switch {
	case lower == "zh", lower == "zh-cn", lower == "zh-sg", strings.HasPrefix(lower, "zh-hans-"):
		return "zh-Hans"
	case lower == "pnb", lower == "pa-arab", strings.HasPrefix(lower, "pa-arab-"), strings.HasPrefix(lower, "pnb-"):
		return "pnb-Arab"
	case lower == "yue", strings.HasPrefix(lower, "yue-"):
		return "yue-Hant"
	case lower == "fil", strings.HasPrefix(lower, "fil-"):
		return "tl"
	}
	base, _, _ := strings.Cut(lower, "-")
	for _, language := range Languages {
		if language.Code == base {
			return base
		}
	}
	return ""
}

// SystemLanguage is used by commands printed before a configuration has been loaded.
func SystemLanguage() string {
	for _, key := range []string{"LEAVESAFE_LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		if code := Normalize(os.Getenv(key)); code != "" {
			return code
		}
	}
	return "en"
}

// Complete prevents partial translations being offered as supported languages.
func Complete(code string) bool {
	source, target := catalogs["en"], catalogs[Normalize(code)]
	if len(source) == 0 || len(target) != len(source) {
		return false
	}
	for key := range source {
		if strings.TrimSpace(target[key]) == "" {
			return false
		}
	}
	return true
}

func Supported() []Language {
	var result []Language
	for _, language := range Languages {
		if Complete(language.Code) {
			result = append(result, language)
		}
	}
	return result
}

// Text keeps machine-supplied diagnostics unchanged. Known UI copy is selected by catalog key.
func Text(code, source string) string {
	normalized := Normalize(code)
	if normalized == "" {
		normalized = "en"
	}
	if message, ok := catalogs[normalized][source]; ok {
		return message
	}
	return source
}
