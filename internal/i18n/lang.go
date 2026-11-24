package i18n

import "strings"

const (
	LangES = "es"
	LangEN = "en"
)

// ParseLang normalizes a language or locale string (e.g. "es", "es-CO", "es_ES.UTF-8",
// "en", "en-US", "en_US.UTF-8") to a supported 2-letter code ("es" or "en").
func ParseLang(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if len(s) < 2 {
		return "", false
	}
	if len(s) > 2 {
		switch s[2] {
		case '-', '_', '.', '@':
		default:
			return "", false
		}
	}
	switch s[:2] {
	case LangES:
		return LangES, true
	case LangEN:
		return LangEN, true
	default:
		return "", false
	}
}

// Resolve determines the active language ("es" or "en") from an explicit preference
// ("auto", "es", "en") and environment variables (LC_ALL > LC_MESSAGES > LANG).
// Non-Spanish or unset locales fall back to "en".
func Resolve(pref string, getenv func(string) string) string {
	p := strings.ToLower(strings.TrimSpace(pref))
	if p == LangES || p == LangEN {
		return p
	}
	if getenv != nil {
		for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			if val := getenv(key); val != "" {
				if lang, ok := ParseLang(val); ok {
					return lang
				}
			}
		}
	}
	return LangEN
}

// Format replaces {key} placeholders in tmpl with values from args.
func Format(tmpl string, args map[string]string) string {
	if len(args) == 0 {
		return tmpl
	}
	pairs := make([]string, 0, len(args)*2)
	for k, v := range args {
		pairs = append(pairs, "{"+k+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
}
