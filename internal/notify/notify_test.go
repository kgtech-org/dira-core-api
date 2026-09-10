package notify

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tmpl(fr, en Text) Template {
	t := Template{Key: "k", Enabled: true, Locales: map[string]Text{}}
	if fr.Body != "" {
		t.Locales[LocaleFR] = fr
	}
	if en.Body != "" {
		t.Locales[LocaleEN] = en
	}
	return t
}

// La langue demandée l'emporte quand elle existe.
func TestPickUsesTheRequestedLocale(t *testing.T) {
	x := tmpl(Text{Title: "Prête", Body: "fr"}, Text{Title: "Ready", Body: "en"})
	assert.Equal(t, "en", pick(x, LocaleEN).Body)
	assert.Equal(t, "fr", pick(x, LocaleFR).Body)
}

// Le repli français est INCONDITIONNEL : un gabarit traduit à moitié part en
// français plutôt que de ne pas partir.
func TestPickFallsBackToFrench(t *testing.T) {
	x := tmpl(Text{Title: "Prête", Body: "fr"}, Text{})
	assert.Equal(t, "fr", pick(x, LocaleEN).Body, "l'anglais manque : le français prend le relais")
	assert.Equal(t, "fr", pick(x, "wo").Body)
	assert.Equal(t, "fr", pick(x, "").Body)
}

// Dernier recours : n'importe quelle langue vaut mieux que rien. Un gabarit
// sans français ne devrait pas exister — la validation le refuse — mais s'il
// arrive de la base, le silence serait pire.
func TestPickTakesAnythingRatherThanNothing(t *testing.T) {
	x := tmpl(Text{}, Text{Title: "Ready", Body: "en"})
	assert.Equal(t, "en", pick(x, "wo").Body)
	assert.Empty(t, pick(Template{Locales: map[string]Text{}}, LocaleFR).Body)
}

// Un appareil déclare sa langue dans une demi-douzaine de formes, et la carte
// des gabarits n'en connaît qu'une.
func TestNormalizeLocale(t *testing.T) {
	cases := map[string]string{
		"fr": "fr", "fr-FR": "fr", "FR_fr": "fr", "EN": "en",
		"": "", "fr-Latn-FR": "fr",
		"zz-very-long-garbage": "zz",
		"12":                   "", // pas une langue
	}
	for in, want := range cases {
		assert.Equal(t, want, normalizeLocale(in), in)
	}
}

// --- envoi ---

type fakePusher struct {
	sent    []PushMessage
	results []PushResult
	err     error
}

func (p *fakePusher) Push(_ context.Context, msgs []PushMessage) ([]PushResult, error) {
	p.sent = append(p.sent, msgs...)
	if p.err != nil {
		return nil, p.err
	}
	if p.results != nil {
		return p.results, nil
	}
	out := make([]PushResult, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, PushResult{Token: m.Token})
	}
	return out, nil
}

// Le titre et le corps sont bornés AVANT l'envoi : laisser le système
// d'exploitation tronquer le ferait couper où il veut, y compris au milieu
// d'un montant.
func TestPushIsBoundedBeforeSending(t *testing.T) {
	long := ""
	for i := 0; i < MaxBody+50; i++ {
		long += "a"
	}
	assert.Len(t, []rune(Truncate(long, MaxBody)), MaxBody)
	assert.Len(t, []rune(Truncate(long, MaxTitle)), MaxTitle)
}

// Le gabarit du CHAT ne nomme jamais l'expéditeur : le canal existe pour que
// le client et le livreur se parlent sans échanger leurs coordonnées, et une
// notification qui nommerait l'un défairait cela sur l'écran verrouillé.
func TestChatTemplateNamesNobody(t *testing.T) {
	x := defaults[KeyChatMessage]
	for loc, txt := range x.Locales {
		vars := Variables(txt.Title, txt.Body)
		assert.Equal(t, []string{"body"}, vars,
			"le gabarit %s ne doit attendre que le corps du message", loc)
	}
}

// Un gabarit compilé n'utilise QUE ce que la plateforme sait remplir. Livrer
// un défaut qui rendrait du vide serait le pire des exemples.
func TestDefaultsUseOnlyProvidedVariables(t *testing.T) {
	for _, d := range Defaults() {
		for loc, txt := range d.Locales {
			assert.Empty(t, unknownVariables(d.Key, txt.Title, txt.Body),
				"gabarit %s/%s : variable que la plateforme ne remplit pas", d.Key, loc)
		}
	}
}

// Toute clé émise par le code déclare ce qu'elle fournit. Sans cette
// déclaration, l'enregistrement refuserait TOUTE variable sur cette clé.
func TestEveryKeyDeclaresWhatItProvides(t *testing.T) {
	for _, d := range Defaults() {
		assert.NotEmpty(t, Provided(d.Key), "clé %s sans variables déclarées", d.Key)
	}
}

// La clé du chat ne fournit QUE le corps : offrir un nom d'expéditeur
// défairait sur l'écran verrouillé ce que la conversation protège.
func TestChatProvidesNoIdentity(t *testing.T) {
	assert.Equal(t, []string{"body"}, Provided(KeyChatMessage))
}

// Tout gabarit compilé porte le FRANÇAIS : c'est le repli, et sans lui un
// destinataire dont la langue manque n'aurait rien à recevoir.
func TestEveryDefaultHasTheFallbackLocale(t *testing.T) {
	for _, d := range Defaults() {
		fr, ok := d.Locales[LocaleFR]
		require.True(t, ok, "gabarit %s sans français", d.Key)
		assert.NotEmpty(t, fr.Body, d.Key)
		assert.NotEmpty(t, fr.Title, d.Key)
	}
}

// Les textes compilés doivent tenir dans les bornes qu'on impose aux textes
// saisis : livrer un défaut que l'écran d'administration refuserait
// d'enregistrer serait incohérent.
func TestDefaultsRespectTheirOwnBounds(t *testing.T) {
	for _, d := range Defaults() {
		for loc, txt := range d.Locales {
			assert.LessOrEqual(t, len([]rune(txt.Title)), MaxTitle, "%s/%s", d.Key, loc)
			assert.LessOrEqual(t, len([]rune(txt.Body)), MaxBody, "%s/%s", d.Key, loc)
		}
	}
}

// Une panne de transport ne doit marquer AUCUN appareil comme mort : les
// jetons n'y sont pour rien, et les désactiver rendrait toute la flotte
// injoignable après un incident réseau.
func TestTransportFailureDisablesNothing(t *testing.T) {
	p := &fakePusher{err: errors.New("fcm down")}
	results, err := p.Push(context.Background(), []PushMessage{{Token: "t1"}})
	require.Error(t, err)
	assert.Nil(t, results, "aucun verdict par appareil : rien à désactiver")
}

// Un jeton déclaré mort par FCM se distingue d'un simple échec : il ne
// guérira pas, et le réessayer indéfiniment finit par faire refuser le projet.
func TestUnregisteredIsDistinctFromAFailure(t *testing.T) {
	p := &fakePusher{results: []PushResult{
		{Token: "dead", Unregistered: true},
		{Token: "flaky", Err: errors.New("timeout")},
		{Token: "ok"},
	}}
	results, err := p.Push(context.Background(), nil)
	require.NoError(t, err)
	assert.True(t, results[0].Unregistered)
	assert.False(t, results[1].Unregistered, "un échec passager n'est pas une mort")
	assert.NoError(t, results[2].Err)
}

// --- catégories et centre de notifications ---

// Une clé de message se range dans une CATÉGORIE : c'est elle que
// l'utilisateur coupe, jamais une clé. Couper `order_delivered` sans couper
// `order_ready` n'aurait aucun sens pour lui.
func TestKeysMapToCategories(t *testing.T) {
	assert.Equal(t, CategoryChatMessages, categoryOf(KeyChatMessage))
	assert.Equal(t, CategoryDriverCall, categoryOf(KeyDriverCall))
	for _, k := range []string{KeyOrderReady, KeyOrderDelivered, KeyOrderCancelled, KeyDispatchFailed} {
		assert.Equal(t, CategoryOrderUpdates, categoryOf(k), k)
	}
}

// ⚠️ L'appel de course n'est PAS coupable : c'est le gagne-pain du livreur, et
// un réglage mal compris le rendrait invisible du dispatch sans qu'il sache
// pourquoi.
func TestTheDriverCallCannotBeMuted(t *testing.T) {
	assert.False(t, Muteable(CategoryDriverCall))
	assert.True(t, Muteable(CategoryOrderUpdates))
	assert.True(t, Muteable(CategoryChatMessages))
	assert.True(t, Muteable(CategoryPromotions))
}

// Toute clé émise se range quelque part : une clé sans catégorie ne pourrait
// ni être coupée ni être classée dans la liste.
func TestEveryKeyHasACategory(t *testing.T) {
	for _, d := range Defaults() {
		assert.NotEmpty(t, categoryOf(d.Key), d.Key)
	}
}
