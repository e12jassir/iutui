package i18n

// Catalog holds all localized strings for a single language.
type Catalog struct {
	Lang   string
	Common CommonStrings
	App    AppStrings
	Views  ViewStrings
}

var (
	esCatalog = Catalog{
		Lang:   LangES,
		Common: esCommonStrings(),
		App:    esAppStrings(),
		Views:  esViewStrings(),
	}
	enCatalog = Catalog{
		Lang:   LangEN,
		Common: enCommonStrings(),
		App:    enAppStrings(),
		Views:  enViewStrings(),
	}
)

// For returns the Catalog for lang ("es" or "en"), falling back to English.
func For(lang string) *Catalog {
	if lang == LangES {
		return &esCatalog
	}
	return &enCatalog
}
