package main

import (
	"embed"
	"unicode"
	"unicode/utf8"
)

//go:embed assets/Font_Awesome_7_BrandsRegular400.otf
//go:embed assets/Font_Awesome_7_FreeRegular400.otf
//go:embed assets/Font_Awesome_7_FreeSolid900.otf
var faFonts embed.FS

type faStyle int

const (
	faSolid  faStyle = iota
	faRegular
	faBrands
)

func (s faStyle) otfPath() string {
	switch s {
	case faSolid:
		return "assets/Font_Awesome_7_FreeSolid900.otf"
	case faRegular:
		return "assets/Font_Awesome_7_FreeRegular400.otf"
	case faBrands:
		return "assets/Font_Awesome_7_BrandsRegular400.otf"
	default:
		return "assets/Font_Awesome_7_FreeSolid900.otf"
	}
}

var faCodepoints map[faStyle]map[string]rune

func init() {
	faCodepoints = make(map[faStyle]map[string]rune)
	faCodepoints[faSolid] = buildFAMap(fa7Icons)
	faCodepoints[faRegular] = buildFAMap(fa7Icons)
	faCodepoints[faBrands] = buildFAMap(fa7BrandsIcons)
}

func buildFAMap(src map[string]string) map[string]rune {
	m := make(map[string]rune, len(src))
	for name, cp := range src {
		r, _ := utf8.DecodeRuneInString(cp)
		camel := camelToKebab(name)
		m[name] = r
		m[camel] = r
		if lower := toLower(name); lower != name {
			m[lower] = r
		}
	}
	return m
}

func camelToKebab(s string) string {
	var out []byte
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			out = append(out, '-')
		}
		out = append(out, byte(unicode.ToLower(r)))
	}
	return string(out)
}

func toLower(s string) string {
	return string(unicode.ToLower(rune(s[0]))) + s[1:]
}
