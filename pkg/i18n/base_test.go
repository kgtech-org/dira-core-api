package i18n

// LE PAQUET QUI LÈVE UN REFUS PORTE SA PHRASE — et ce test le fait respecter.
//
// ⚠️ Un refus déclaré dans `pkg/*` sort dans TOUS les services. S'il n'est
// traduit que dans le `locales/` d'un seul, les autres servent l'anglais par
// défaut, et rien ne le signale : le code compile, les tests passent, le
// service répond 401. C'est la phrase, et elle seule, qui est fausse.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var declared = regexp.MustCompile(`apperr\.(?:New|Conflict|NotFound|Forbidden|Unauthorized|PaymentRequired|TooManyRequests)\(\s*"([a-z_]+)"`)

// exempt : les codes que la base ne traduit pas, et pourquoi.
var exempt = map[string]string{
	// Le refus de cadence est traduit par chaque service dans son propre
	// fichier depuis le premier jour ; il n'a jamais manqué nulle part.
	"rate_limited": "déjà traduit dans chaque service, historiquement",
}

func TestEverySharedRefusalIsTranslatedInTheBase(t *testing.T) {
	codes := map[string]string{}
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range declared.FindAllStringSubmatch(string(src), -1) {
			codes[m[1]] = path
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, codes, "aucun code trouvé dans pkg/ : le motif de lecture est cassé, pas le code")

	fr := keysOf(t, "base/fr.toml")
	en := keysOf(t, "base/en.toml")
	for code, where := range codes {
		if reason, ok := exempt[code]; ok {
			t.Logf("%s : hors base — %s", code, reason)
			continue
		}
		assert.True(t, fr[code], "`%s` (levé dans %s) n'a pas de phrase FRANÇAISE dans pkg/i18n/base/fr.toml", code, where)
		assert.True(t, en[code], "`%s` (levé dans %s) n'a pas de phrase ANGLAISE dans pkg/i18n/base/en.toml", code, where)
	}
}

// ⚠️ LE FICHIER DU SERVICE L'EMPORTE SUR LA BASE, ET LA BASE COMBLE CE QU'IL NE
// DIT PAS. Dans l'autre ordre, la base serait impossible à surcharger — et un
// service qui voudrait dire « Jeton expiré, reconnectez-vous » à sa manière ne
// le pourrait plus.
func TestTheServiceFileOverridesTheBaseAndTheBaseFillsTheGaps(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fr.toml"), "[errors.invalid_token]\nother = \"Phrase du service\"\n")
	write(t, filepath.Join(dir, "en.toml"), "[errors.invalid_token]\nother = \"Service wording\"\n")

	tr, err := New(dir)
	require.NoError(t, err)

	got, ok := tr.Translate("fr", "errors.invalid_token", nil)
	require.True(t, ok)
	assert.Equal(t, "Phrase du service", got, "le service surcharge la base")

	got, ok = tr.Translate("fr", "errors.session_superseded", nil)
	require.True(t, ok, "la base comble ce que le service ne dit pas")
	assert.Contains(t, got, "autre appareil", "et c'est bien du français qui sort")
}

func keysOf(t *testing.T, path string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`\[errors\.([a-z_]+)\]`).FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = true
	}
	return out
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
