// Package i18n localizes API messages using go-i18n with TOML locale files
// (locales/fr.toml, locales/en.toml). French is the default language for the
// target market; English is the fallback.
package i18n

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

const DefaultLanguage = "fr"

var supported = []string{"fr", "en"}

type ctxKey struct{}

type Translator struct {
	bundle *goi18n.Bundle
}

// base porte les refus que `pkg/*` prononce AU NOM DE TOUS LES SERVICES.
//
// ⚠️ EMBARQUÉ, PAS LU SUR LE DISQUE. Un refus levé par `pkg/middleware` sort
// dans les trois services ; sa phrase ne peut donc pas vivre dans le
// `locales/` d'un seul. Elle vivait dans celui du socle, et les courses comme
// la livraison servaient l'anglais par défaut : « this device is no longer the
// one signed in to this account » à un chauffeur dont l'application est en
// français. Relevé par l'équipe mobile le 29 septembre 2026 — et dix des
// dix-sept refus partagés n'étaient traduits NULLE PART.
//
// Le paquet qui lève le refus porte sa phrase. Chargé AVANT les fichiers du
// service, qui peuvent le surcharger clé par clé, jamais l'oublier.
//
//go:embed base/*.toml
var base embed.FS

// New loads the shared base bundle, then the service's locale files on top.
// Missing or unparsable service files are an error; a key absent everywhere
// falls back to the message's default text.
func New(localesPath string) (*Translator, error) {
	bundle := goi18n.NewBundle(language.French)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	// ⚠️ LA BASE D'ABORD. go-i18n garde la DERNIÈRE définition d'une clé : le
	// fichier du service, chargé ensuite, l'emporte donc sur la base. L'ordre
	// inverse rendrait la base impossible à surcharger.
	for _, lang := range supported {
		data, err := base.ReadFile("base/" + lang + ".toml")
		if err != nil {
			return nil, fmt.Errorf("i18n: base %s: %w", lang, err)
		}
		if _, err := bundle.ParseMessageFileBytes(data, "base."+lang+".toml"); err != nil {
			return nil, fmt.Errorf("i18n: parse base %s: %w", lang, err)
		}
	}

	for _, lang := range supported {
		file := filepath.Join(localesPath, lang+".toml")
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("i18n: read %s: %w", file, err)
		}
		if _, err := bundle.ParseMessageFileBytes(data, file); err != nil {
			return nil, fmt.Errorf("i18n: parse %s: %w", file, err)
		}
	}
	return &Translator{bundle: bundle}, nil
}

// Translate resolves key in the given language; ok is false when the key is
// unknown, letting callers fall back to a default message.
func (t *Translator) Translate(lang, key string, data map[string]any) (string, bool) {
	localizer := goi18n.NewLocalizer(t.bundle, lang, DefaultLanguage)
	msg, err := localizer.Localize(&goi18n.LocalizeConfig{MessageID: key, TemplateData: data})
	if err != nil {
		return "", false
	}
	return msg, true
}

// DetectLanguage picks the first supported language from an Accept-Language
// header ("fr-FR,fr;q=0.9,en;q=0.7"), defaulting to French.
func DetectLanguage(header string) string {
	for _, part := range strings.Split(header, ",") {
		code := strings.TrimSpace(strings.Split(part, ";")[0])
		code = strings.ToLower(strings.Split(code, "-")[0])
		for _, s := range supported {
			if code == s {
				return s
			}
		}
	}
	return DefaultLanguage
}

// WithLang stores the request language in the context.
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, ctxKey{}, lang)
}

// LangFromContext returns the request language, defaulting to French.
func LangFromContext(ctx context.Context) string {
	if lang, ok := ctx.Value(ctxKey{}).(string); ok && lang != "" {
		return lang
	}
	return DefaultLanguage
}
