package sos

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LE FILTRE DE LA FILE — éprouvé en DONNÉES, parce qu'un filtre MongoDB a l'air
// juste, répond sans erreur, et ne rend pas les bonnes lignes.

// matches dit si un document satisfait le filtre. Une évaluation volontairement
// minuscule : elle ne couvre que les opérateurs qu'on utilise ici (`$in`,
// `$gte`, `$or`), et c'est suffisant pour attraper la seule chose qui compte —
// quelles alertes entrent dans la file.
func matches(doc bson.M, q bson.M) bool {
	for k, want := range q {
		if k == "$or" {
			any := false
			for _, sub := range want.([]bson.M) {
				if matches(doc, sub) {
					any = true
					break
				}
			}
			if !any {
				return false
			}
			continue
		}
		got, present := doc[k]
		switch w := want.(type) {
		case bson.M:
			if in, ok := w["$in"]; ok {
				found := false
				for _, v := range in.([]string) {
					if present && got == v {
						found = true
					}
				}
				if !found {
					return false
				}
			}
			if gte, ok := w["$gte"]; ok {
				t, isTime := got.(time.Time)
				if !present || !isTime || t.Before(gte.(time.Time)) {
					return false
				}
			}
		default:
			if !present || got != want {
				return false
			}
		}
	}
	return true
}

// ⚠️⚠️ LA LIGNE QUI COMPTE : UNE ALERTE ANNULÉE PAR LA PERSONNE RESTE DANS LA
// FILE. Une annulation peut être CONTRAINTE — quelqu'un à qui on arrache son
// téléphone et qui le voit annuler sous ses yeux. C'est le scénario même que ce
// bouton existe pour couvrir, et le faire disparaître de l'écran le rendrait
// invisible.
func TestAnAlarmCancelledByThePersonStaysInTheQueue(t *testing.T) {
	now := time.Date(2026, 10, 8, 21, 0, 0, 0, time.UTC)
	q := listQuery(Filter{Live: true}, now)

	justCancelled := bson.M{
		"status": StatusClosed, "closed_by": ClosedByRaiser,
		"closed_at": now.Add(-4 * time.Second),
	}
	assert.True(t, matches(justCancelled, q),
		"annulée il y a quatre secondes : c'est le signal le plus inquiétant, pas un non-événement")

	// ⚠️ ET ELLE SORT APRÈS QUINZE MINUTES, sinon la file se remplit de ce qui
	// est réglé et l'alarme du jour se noie dedans.
	old := bson.M{
		"status": StatusClosed, "closed_by": ClosedByRaiser,
		"closed_at": now.Add(-20 * time.Minute),
	}
	assert.False(t, matches(old, q))
}

// Une alerte refermée par l'EXPLOITATION sort tout de suite : quelqu'un l'a
// traitée et a écrit un dénouement — la garder à l'écran ferait rappeler des
// gens déjà rappelés.
func TestAnAlarmClosedByOperationsLeavesTheQueueAtOnce(t *testing.T) {
	now := time.Now().UTC()
	q := listQuery(Filter{Live: true}, now)
	assert.False(t, matches(bson.M{
		"status": StatusClosed, "closed_by": ClosedByStaff, "closed_at": now,
	}, q))
}

// Ouvertes et prises sont toutes les deux dans la file : « prise » n'est pas
// « finie », et un opérateur qui part en pause doit la laisser visible.
func TestOpenAndAcknowledgedAreBothInTheQueue(t *testing.T) {
	q := listQuery(Filter{Live: true}, time.Now().UTC())
	assert.True(t, matches(bson.M{"status": StatusOpen}, q))
	assert.True(t, matches(bson.M{"status": StatusAcknowledged}, q))
}

// L'historique filtre sur un statut donné et n'a pas la clause `$or` de la
// file : sans cela, demander « les fermées » aurait rendu les ouvertes aussi.
func TestHistoryFiltersOnTheStatusItWasAsked(t *testing.T) {
	q := listQuery(Filter{Status: StatusClosed}, time.Now().UTC())
	require.NotContains(t, q, "$or")
	assert.Equal(t, StatusClosed, q["status"])
	assert.True(t, matches(bson.M{"status": StatusClosed}, q))
	assert.False(t, matches(bson.M{"status": StatusOpen}, q))
}

// `since` et `user_id` s'ajoutent sans effacer le reste — c'est ainsi qu'on
// répond à « ce chauffeur en déclenche-t-il une par semaine ? ».
func TestTheHistoryOfOnePersonCanBeAskedOverAWindow(t *testing.T) {
	uid := primitive.NewObjectID()
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	q := listQuery(Filter{UserID: &uid, Since: &since}, time.Now().UTC())

	assert.True(t, matches(bson.M{"user_id": uid, "created_at": since.Add(time.Hour)}, q))
	assert.False(t, matches(bson.M{"user_id": uid, "created_at": since.Add(-time.Hour)}, q),
		"hors fenêtre")
	assert.False(t, matches(bson.M{"user_id": primitive.NewObjectID(), "created_at": since.Add(time.Hour)}, q),
		"quelqu'un d'autre")
}
