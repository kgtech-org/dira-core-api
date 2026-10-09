package user

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// appsAllowedOn lit la liste `oneof` du champ `App` d'une structure de requête.
func appsAllowedOn(t *testing.T, req any) []string {
	t.Helper()
	field, ok := reflect.TypeOf(req).FieldByName("App")
	require.True(t, ok, "cette requête ne porte pas de champ `App`")
	for _, rule := range strings.Split(field.Tag.Get("validate"), ",") {
		if after, found := strings.CutPrefix(rule, "oneof="); found {
			out := strings.Fields(after)
			sort.Strings(out)
			return out
		}
	}
	t.Fatal("le champ `App` n'a pas de règle `oneof` : toute valeur passerait")
	return nil
}

// ⚠️⚠️ LE TEST QUI MANQUAIT. Le rôle `partner` existait, `appRole` le
// connaissait, la console l'envoyait — et la validation du corps de
// `POST /auth/login` le refusait par un `422` sur le champ `app`. Un rôle
// complet, et aucune porte pour entrer.
//
// Personne ne l'a vu en relisant le code : les deux listes vivent dans deux
// fichiers, et chacune est juste de son côté. Ça s'est vu en ouvrant la console
// pour de vrai. Ce test fige leur accord, pour le prochain rôle.
func TestEveryKnownAppCanLogIn(t *testing.T) {
	known := make([]string, 0, len(appRole))
	for app := range appRole {
		known = append(known, app)
	}
	sort.Strings(known)

	assert.Equal(t, known, appsAllowedOn(t, LoginRequest{}),
		"la connexion doit admettre EXACTEMENT les applications que `appRole` connaît : "+
			"une de moins est un rôle sans porte, une de plus est une valeur que rien ne sait traduire")
}

// ⚠️ ET L'INSCRIPTION N'ADMET PAS `partner`, délibérément : `Register` refuse ce
// rôle, donc l'accepter ici n'ouvrirait rien et ferait croire le contraire. Le
// contrôle est NÉGATIF — sans lui, « ajouter partner partout » passerait au
// vert.
func TestRegistrationRefusesThePartnerApp(t *testing.T) {
	for _, req := range []any{RegisterRequest{}, OTPRequest{}, OTPVerifyRequest{}} {
		assert.NotContains(t, appsAllowedOn(t, req), "partner",
			"un partenaire ne s'inscrit pas lui-même : son accès s'ouvre depuis la fiche de sa flotte")
	}
}
