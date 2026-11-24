package i18n

// CommonStrings holds shared UI strings such as language selection.
type CommonStrings struct {
	LangLabel        string
	LangDesc         string
	LangAuto         string
	LangES           string
	LangEN           string
	LangChangedToast string
}

func esCommonStrings() CommonStrings {
	return CommonStrings{
		LangLabel:        "Idioma",
		LangDesc:         "Idioma de la interfaz (auto detecta el locale del sistema)",
		LangAuto:         "auto",
		LangES:           "español",
		LangEN:           "english",
		LangChangedToast: "Idioma: {lang}",
	}
}

func enCommonStrings() CommonStrings {
	return CommonStrings{
		LangLabel:        "Language",
		LangDesc:         "Interface language (auto detects system locale)",
		LangAuto:         "auto",
		LangES:           "español",
		LangEN:           "english",
		LangChangedToast: "Language: {lang}",
	}
}
