package indexes

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/kgtech-org/dira-core-api/pkg/db"
)

// Ce test lit indexes/*.mongodb.js et exige une correspondance EXACTE avec les
// déclarations Go.
//
// C'est la protection qui manquait : les fichiers .js décrivaient 69 index que
// rien n'appliquait, et personne ne pouvait le voir. Désormais, ajouter un
// index d'un seul côté fait échouer la compilation du contrat entre les deux.

// createIndexCall capture `db.<collection>.createIndex({keys}, {options})`.
// Les options admettent UN niveau d'imbrication : un filtre partiel s'écrit
// `{ unique: true, partialFilterExpression: { email: { $gt: "" } } }`. Sans
// cela, l'index n'était tout simplement pas vu, et la parité échouait par
// comptage sans dire lequel manquait.
var createIndexCall = regexp.MustCompile(
	`db\.(\w+)\.createIndex\(\s*(\{[^{}]*\})\s*(?:,\s*(\{(?:[^{}]|\{(?:[^{}]|\{[^{}]*\})*\})*\})\s*)?,?\s*\)`)

// signature normalise un index en une chaîne comparable :
// "stores{location:2dsphere}" ou "users{email:1}+sparse+unique".
func signature(collection, keys string, flags []string) string {
	sort.Strings(flags)
	s := collection + "{" + keys + "}"
	for _, f := range flags {
		s += "+" + f
	}
	return s
}

// parseJSKeys transforme `{ store_id: 1, dish_id: 1 }` en "store_id:1,dish_id:1".
func parseJSKeys(t *testing.T, raw string) string {
	t.Helper()
	inner := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "{}"))
	var parts []string
	for _, field := range strings.Split(inner, ",") {
		name, value, ok := strings.Cut(field, ":")
		require.True(t, ok, "champ d'index illisible : %q", field)
		name = strings.Trim(strings.TrimSpace(name), `"'`)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		parts = append(parts, name+":"+value)
	}
	return strings.Join(parts, ",")
}

// parseJSOptions ne retient que les options qui changent le comportement de
// l'index. Un commentaire ou une option décorative ne doit pas faire échouer
// la comparaison.
func parseJSOptions(t *testing.T, raw string) []string {
	t.Helper()
	if raw == "" {
		return nil
	}
	var flags []string
	if strings.Contains(raw, "unique: true") {
		flags = append(flags, "unique")
	}
	if strings.Contains(raw, "sparse: true") {
		flags = append(flags, "sparse")
	}
	// Un filtre partiel change ce que l'index couvre : deux index de mêmes clés
	// dont l'un filtre et l'autre non ne sont pas le même index.
	if strings.Contains(raw, "partialFilterExpression") {
		flags = append(flags, "partial")
	}
	if m := regexp.MustCompile(`expireAfterSeconds:\s*(\d+)`).FindStringSubmatch(raw); m != nil {
		flags = append(flags, "ttl="+m[1])
	}
	return flags
}

// declaredInJS reads every index declared by the documentation files.
func declaredInJS(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "indexes", "*.mongodb.js"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "aucun fichier d'index trouvé : le chemin a-t-il changé ?")

	out := map[string]string{}
	for _, path := range files {
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		// Les lignes commentées décrivent des intentions, pas des index posés.
		var live []string
		for _, line := range strings.Split(string(body), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "//") {
				live = append(live, line)
			}
		}
		for _, m := range createIndexCall.FindAllStringSubmatch(strings.Join(live, "\n"), -1) {
			sig := signature(m[1], parseJSKeys(t, m[2]), parseJSOptions(t, m[3]))
			out[sig] = filepath.Base(path)
		}
	}
	return out
}

// declaredInGo renders the Go table with the same normalisation.
func declaredInGo(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, spec := range applicationIndexes {
		var parts []string
		for _, e := range spec.Keys {
			parts = append(parts, fmt.Sprintf("%s:%v", e.Key, e.Value))
		}
		var flags []string
		if spec.Unique {
			flags = append(flags, "unique")
		}
		if spec.Sparse {
			flags = append(flags, "sparse")
		}
		if spec.PartialFilter != nil {
			flags = append(flags, "partial")
		}
		if spec.TTLSeconds != nil {
			flags = append(flags, fmt.Sprintf("ttl=%d", *spec.TTLSeconds))
		}
		sig := signature(spec.Collection, strings.Join(parts, ","), flags)
		assert.False(t, out[sig], "index déclaré deux fois côté Go : %s", sig)
		out[sig] = true
	}
	return out
}

func TestGoIndexesMatchTheDocumentedSchema(t *testing.T) {
	js := declaredInJS(t)
	golang := declaredInGo(t)

	for sig, file := range js {
		assert.True(t, golang[sig],
			"%s déclare %s, que le code n'applique pas — c'est exactement le défaut qui a\n"+
				"provoqué le 500 sur GET /stores : un index documenté mais jamais posé.", file, sig)
	}
	for sig := range golang {
		assert.NotEmpty(t, js[sig],
			"le code applique %s, absent de indexes/*.mongodb.js — la documentation du schéma doit suivre", sig)
	}
	assert.Equal(t, len(js), len(golang), "les deux sources doivent décrire le même nombre d'index")
}

// L'unicité du TÉLÉPHONE est l'identité de la plateforme : on s'inscrit par
// téléphone, et deux comptes sur le même numéro rendraient la connexion
// ambiguë. Un test nommé pour qu'une suppression accidentelle dise pourquoi
// elle casse.
func TestPhoneUniquenessIsDeclared(t *testing.T) {
	assert.True(t, declaredInGo(t)["users{phone:1}+unique"],
		"sans cet index, deux comptes peuvent porter le même numéro et la connexion devient ambiguë")
}

// Les jetons de rafraîchissement PURGENT tout seuls : sans TTL, la collection
// grossit indéfiniment de jetons que plus personne ne peut présenter.
func TestRefreshTokensExpireOnTheirOwn(t *testing.T) {
	for _, spec := range applicationIndexes {
		if spec.Collection == "refresh_tokens" && spec.TTLSeconds != nil {
			assert.Equal(t, "expires_at", spec.Keys[0].Key,
				"le TTL doit porter sur la date d'expiration du jeton, pas sur sa création")
			return
		}
	}
	t.Fatal("aucun index TTL sur refresh_tokens : la collection ne se purgerait jamais")
}

// Les clés d'un index composite sont ORDONNÉES : MongoDB n'utilise un index
// composite que sur un PRÉFIXE de ses clés, donc l'ordre fait partie du
// contrat, pas de la présentation.
func TestCompositeKeyOrderIsPreserved(t *testing.T) {
	for _, spec := range applicationIndexes {
		if spec.Collection == "user_addresses" && len(spec.Keys) == 3 {
			assert.Equal(t, []string{"user_id", "is_default", "created_at"},
				[]string{spec.Keys[0].Key, spec.Keys[1].Key, spec.Keys[2].Key},
				"le carnet se lit par compte, l'adresse par défaut en tête : c'est celle que l'application présélectionne")
			return
		}
	}
	t.Fatal("index composite du carnet d'adresses introuvable")
}

func TestIndexKeyHelperRejectsMalformedInput(t *testing.T) {
	assert.Panics(t, func() { db.K("phone") }, "un nom sans direction est une faute de programmation")
	assert.Panics(t, func() { db.K(1, "phone") }, "le nom du champ doit être une chaîne")
	assert.NotPanics(t, func() { db.K("phone", 1, "role", -1) })
}

// Sparse n'écarte que les champs ABSENTS, jamais une chaîne vide. Sur une
// plateforme où l'on s'inscrit par téléphone, la plupart des comptes n'ont pas
// d'e-mail : deux comptes portant `email: ""` suffisaient à empêcher l'index
// unique de se construire, et l'API démarrait sans lui — en le journalisant,
// mais en démarrant quand même.
func TestUserEmailIndexIgnoresEmptyEmails(t *testing.T) {
	var spec *db.Index
	for i := range applicationIndexes {
		s := applicationIndexes[i]
		if s.Collection == "users" && len(s.Keys) == 1 && s.Keys[0].Key == "email" {
			spec = &applicationIndexes[i]
			break
		}
	}
	require.NotNil(t, spec, "l'index d'unicité de l'e-mail doit exister")
	assert.True(t, spec.Unique, "un e-mail identifie un compte : il doit rester unique")
	require.NotNil(t, spec.PartialFilter,
		"sparse ne suffit pas : il laisse passer les chaînes vides, qui entrent alors en collision")
	assert.False(t, spec.Sparse,
		"sparse ET partiel ensemble est refusé par MongoDB")

	// `$ne` n'est pas admis dans une expression de filtre partiel : `$gt: \"\"`
	// est la façon de dire « chaîne non vide ».
	raw, err := bson.MarshalExtJSON(spec.PartialFilter, false, false)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "$gt")
	assert.NotContains(t, string(raw), "$ne")
}
