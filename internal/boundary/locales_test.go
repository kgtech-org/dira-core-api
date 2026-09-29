package boundary_test

// TOUT REFUS QUE CE SERVICE PRONONCE DOIT SAVOIR SE DIRE EN FRANÇAIS.
//
// ⚠️ POURQUOI CE FICHIER EXISTE ICI AUSSI. Le 26 septembre 2026, onze codes des
// courses sont partis en production sans traduction, et un test a été posé — dans
// ce seul dépôt-là. Le 29 septembre, l'équipe mobile a relevé un refus en
// anglais ; en mesurant, on en a trouvé UNE CENTAINE dans les trois autres
// services : `insufficient_funds`, `store_closed`, `debt_over_limit`… Un client
// togolais lisait « insufficient funds » dans une application en français.
//
// Rien ne le signalait : le code compile, les tests passent, le service répond
// 402. C'est la phrase, et elle seule, qui est fausse — et personne ne la relit
// avant que quelqu'un ne la voie sur son téléphone.
//
// Le test lit les codes DANS LE CODE (les appels à `apperr.*`) et vérifie que
// chacun a sa phrase dans les deux langues. ⚠️ Il ne regarde que CE dépôt : les
// refus que le socle prononce au nom de tous vivent dans `pkg/i18n/base`, chargé
// avant `locales/`, et sont gardés par un test là-bas.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ⚠️ `Validation` et `Internal` sont ABSENTS de la liste, volontairement : le
// premier porte toujours `validation_failed`, le second `internal_error`, tous
// deux traduits depuis le premier jour.
var declared = regexp.MustCompile(`apperr\.(?:New|Conflict|NotFound|Forbidden|Unauthorized|PaymentRequired|TooManyRequests)\(\s*"([a-z_]+)"`)

// exempt : les codes qu'on ne traduit pas, et pourquoi.
var exempt = map[string]string{
	// Porte de service : l'appelant est une machine, pas une personne.
	"unauthorized": "réponse à un service, jamais montrée à quelqu'un",
}

func TestEveryRefusalCanSpeakFrench(t *testing.T) {
	codes := map[string]string{}
	err := filepath.Walk("../..", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		// ⚠️ `pkg/` est exclu : ses refus sont ceux du socle POUR TOUS, traduits
		// dans le paquet de base et gardés par leur propre test.
		if strings.Contains(path, string(filepath.Separator)+"pkg"+string(filepath.Separator)) {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
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
	require.NotEmpty(t, codes, "aucun code trouvé : le motif de lecture est cassé, pas le code")

	fr := readLocale(t, "../../locales/fr.toml")
	en := readLocale(t, "../../locales/en.toml")

	for code, where := range codes {
		if reason, ok := exempt[code]; ok {
			t.Logf("%s : non traduit — %s", code, reason)
			continue
		}
		assert.True(t, fr[code], "`%s` (déclaré dans %s) n'a pas de phrase FRANÇAISE dans locales/fr.toml", code, where)
		assert.True(t, en[code], "`%s` (déclaré dans %s) n'a pas de phrase ANGLAISE dans locales/en.toml", code, where)
	}
}

func readLocale(t *testing.T, path string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`\[errors\.([a-z_]+)\]`).FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = true
	}
	return out
}
