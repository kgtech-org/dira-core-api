package country

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// LE VERROU EST UNE POLITIQUE, PAS UN CONTRÔLE D'ACCÈS — et une politique doit
// arriver COMPLÈTE : une application ne doit jamais avoir à décider ce qu'un
// champ vide veut dire.

func TestAnUnsetCountryGetsAWholePolicy(t *testing.T) {
	l := appLockResponse(AppLock{})
	assert.Equal(t, LockOptional, l.Mode, "proposé, jamais imposé par défaut")
	assert.True(t, l.Biometrics)
	assert.Equal(t, 4, l.PINLength)
	assert.Equal(t, 120, l.GraceSeconds)
	assert.Equal(t, 5, l.MaxAttempts)
}

func TestTheCountrysLockChoiceIsServedAsIs(t *testing.T) {
	no := false
	l := appLockResponse(AppLock{
		Mode: LockRequired, Biometrics: &no, PINLength: 6,
		GraceSeconds: 30, MaxAttempts: 3,
	})
	assert.Equal(t, LockRequired, l.Mode)
	assert.False(t, l.Biometrics, "code secret seul")
	assert.Equal(t, 6, l.PINLength)
	assert.Equal(t, 30, l.GraceSeconds)
	assert.Equal(t, 3, l.MaxAttempts)
}

// ⚠️ « NON RÉGLÉ » ET « REFUSÉE » NE SONT PAS LA MÊME CHOSE. Un `bool` nu
// aurait interdit la biométrie dans tout pays enregistré avant ce réglage.
func TestBiometricsDistinguishesUnsetFromRefused(t *testing.T) {
	assert.True(t, appLockResponse(AppLock{}).Biometrics, "non réglé = admise")
	no := false
	assert.False(t, appLockResponse(AppLock{Biometrics: &no}).Biometrics)
	yes := true
	assert.True(t, appLockResponse(AppLock{Biometrics: &yes}).Biometrics)
}

// ⚠️ UN PAYS QUI A VOULU ZÉRO GARDE ZÉRO. Redemander le code à chaque retour
// d'arrière-plan est un choix légitime (téléphone partagé) ; le confondre avec
// « non réglé » aurait rendu ce choix impossible à exprimer.
func TestAZeroGraceChosenOnPurposeIsKept(t *testing.T) {
	l := appLockResponse(AppLock{Mode: LockRequired, GraceSeconds: 0, MaxAttempts: 5})
	assert.Equal(t, 0, l.GraceSeconds)
}

// Un réglage vide est refusé plutôt qu'accepté sans rien faire : un écran qui
// dit « enregistré » sans avoir rien changé est pire qu'un refus.
func TestAnEmptySecurityUpdateIsRefused(t *testing.T) {
	assert.Equal(t, "validation_failed", errNoSecurityUpdate.Code)
	assert.Contains(t, errNoSecurityUpdate.Message, "mode")
}
