package fleet

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

func bp(v int) *int { return &v }

// ⚠️ LE TEST QUI COMPTE. `nil` et `0` ne veulent pas dire la même chose : nil
// signifie « cette flotte suit le taux de la plateforme », zéro signifie « une
// gratuité a été négociée ». Les confondre ferait travailler gratuitement
// toute flotte enregistrée sans taux — une perte sèche que rien ne signale,
// parce qu'un zéro est une valeur parfaitement valide.
func TestCommissionDistinguishesUnsetFromZero(t *testing.T) {
	assert.NoError(t, checkCommission(nil), "non renseigné : la flotte suivra le taux de la plateforme")
	assert.NoError(t, checkCommission(bp(0)), "zéro est une gratuité NÉGOCIÉE, pas une absence")
	assert.NoError(t, checkCommission(bp(10000)))

	// Et le pointeur le PORTE jusqu'à la réponse : un `int` nu aurait rendu
	// les deux cas identiques à la lecture.
	unset := toResponse(&Fleet{})
	assert.Nil(t, unset.CommissionBp, "non renseigné doit rester distinguable à la lecture")
	free := toResponse(&Fleet{CommissionBp: bp(0)})
	require.NotNil(t, free.CommissionBp)
	assert.Equal(t, 0, *free.CommissionBp)
}

// Un taux hors bornes est un taux qui ne veut rien dire. 12 000 dix-millièmes
// prélèveraient 120 % d'une course.
func TestCommissionIsBounded(t *testing.T) {
	for _, v := range []int{-1, 10001, 999999} {
		err := checkCommission(bp(v))
		require.Error(t, err, "%d devrait être refusé", v)
		assert.Equal(t, "validation_failed", apperr.From(err).Code)
	}
}

// ⚠️ La recherche est une entrée NON FIABLE. Sans échappement, un nom
// contenant une parenthèse fait échouer la requête, et `.*` parcourt la
// collection entière.
func TestSearchIsEscaped(t *testing.T) {
	assert.Equal(t, `Sodigaz \(Lomé\)`, regexEscape("Sodigaz (Lomé)"))
	assert.Equal(t, `\.\*`, regexEscape(".*"))
	assert.Equal(t, `Transports Kodjo`, regexEscape("Transports Kodjo"), "un nom ordinaire n'est pas altéré")
}

// La réponse porte l'identifiant du gérant quand il existe, et RIEN quand il
// n'existe pas — une chaîne vide dans un champ d'identifiant se lit comme un
// compte introuvable, alors que la flotte n'en a simplement pas.
func TestOwnerIsOmittedWhenAbsent(t *testing.T) {
	assert.Empty(t, toResponse(&Fleet{}).OwnerUserID)
}
