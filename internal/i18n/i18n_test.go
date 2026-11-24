package i18n

import (
	"reflect"
	"strings"
	"testing"
)

func checkStructStrings(t *testing.T, lang string, v reflect.Value, path string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			t.Fatalf("%s: nil pointer at %s", lang, path)
		}
		checkStructStrings(t, lang, v.Elem(), path)
	case reflect.Struct:
		typ := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			checkStructStrings(t, lang, v.Field(i), path+"."+f.Name)
		}
	case reflect.String:
		s := v.String()
		if strings.TrimSpace(s) == "" {
			t.Errorf("%s: empty string at %s", lang, path)
		}
		if strings.Contains(s, "%") {
			t.Errorf("%s: unexpected '%%' printf verb in %s: %q", lang, path, s)
		}
	}
}

func TestCatalogParity(t *testing.T) {
	for _, lang := range []string{LangES, LangEN} {
		cat := For(lang)
		if cat.Lang != lang {
			t.Fatalf("For(%q).Lang = %q, want %q", lang, cat.Lang, lang)
		}
		checkStructStrings(t, lang, reflect.ValueOf(cat), "Catalog")
	}
}

func TestResolvePrecedence(t *testing.T) {
	tests := []struct {
		name string
		pref string
		env  map[string]string
		want string
	}{
		{
			name: "explicit es overrides env",
			pref: "es",
			env:  map[string]string{"LC_ALL": "en_US.UTF-8"},
			want: "es",
		},
		{
			name: "explicit en overrides env",
			pref: "EN",
			env:  map[string]string{"LC_ALL": "es_CO.UTF-8"},
			want: "en",
		},
		{
			name: "auto LC_ALL beats LC_MESSAGES and LANG",
			pref: "auto",
			env:  map[string]string{"LC_ALL": "es_ES.UTF-8", "LC_MESSAGES": "en_US.UTF-8", "LANG": "en_US.UTF-8"},
			want: "es",
		},
		{
			name: "auto LC_MESSAGES beats LANG",
			pref: "auto",
			env:  map[string]string{"LC_MESSAGES": "es_CO.UTF-8", "LANG": "en_US.UTF-8"},
			want: "es",
		},
		{
			name: "auto LANG used when LC_ALL and LC_MESSAGES unset",
			pref: "auto",
			env:  map[string]string{"LANG": "es-419"},
			want: "es",
		},
		{
			name: "auto non-Spanish locale falls back to en",
			pref: "auto",
			env:  map[string]string{"LC_ALL": "fr_FR.UTF-8"},
			want: "en",
		},
		{
			name: "empty pref and empty env falls back to en",
			pref: "",
			env:  map[string]string{},
			want: "en",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.pref, func(k string) string { return tc.env[k] })
			if got != tc.want {
				t.Errorf("Resolve(%q, env) = %q, want %q", tc.pref, got, tc.want)
			}
		})
	}
}

func TestFormat(t *testing.T) {
	got := Format("Idioma: {lang} ({code})", map[string]string{
		"lang": "español",
		"code": "es",
	})
	want := "Idioma: español (es)"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}
