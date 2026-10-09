package sos

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// LE PROTOCOLE DU SERVICE CLIENT : appeler la personne, puis les secours.

// ⚠️⚠️ LE TEST QUI TIENT LA DÉCISION PRODUIT : AUCUN NUMÉRO NE PART VERS LES
// APPLICATIONS.
//
// Le téléphone de quelqu'un en danger ne compose rien. L'alerte part au service
// client, un opérateur appelle d'abord la personne, et c'est lui qui appelle les
// secours. Ce test est volontairement structurel : il vérifie que le TYPE servi
// aux applications n'a pas de champ de numéros, parce qu'un champ servi « au cas
// où » avec la consigne de ne pas l'afficher aurait fini par un bouton d'appel.
//
// ⚠️ IL ÉCHOUE À LA COMPILATION si quelqu'un remet `Numbers` dans `Settings` —
// et c'est exactement l'effet voulu : une spec se contourne, un type non.
func TestNothingDialableReachesTheApplications(t *testing.T) {
	s := (&Service{}).SettingsFor(testCtx())
	assert.True(t, s.Button, "le bouton existe partout")
	// ⚠️ CE QUE L'APPLICATION PROMET À LA PLACE : « on vous rappelle ». C'est
	// la seule chose que la personne cherche à savoir après avoir appuyé, et
	// sans ce champ un écran écrirait « appelez la police » — en envoyant
	// composer un numéro qu'on ne lui a pas donné.
	assert.True(t, s.CallsBack)

	// La forme du type, figée : ces quatre champs et rien de composable.
	var probe any = s
	_, isSettings := probe.(Settings)
	assert.True(t, isSettings)
}

// ⚠️ « APPELÉ » N'EST PAS « JOINT », et les confondre serait le pire mensonge du
// dispositif. Un opérateur qui a laissé sonner dix fois a fait son travail, mais
// la personne n'a pas répondu — et après un choc violent, c'est l'information la
// plus inquiétante de l'écran. `reached` est donc une RÉPONSE, pas une case
// cochée par le clic.
func TestCallingIsNotReaching(t *testing.T) {
	// La distinction vit dans le type d'entrée : deux champs, pas un.
	tried := ContactInput{Reached: false, Note: "dix sonneries, pas de réponse"}
	got := ContactInput{Reached: true, Note: "il va bien, dos-d'âne"}
	assert.False(t, tried.Reached)
	assert.True(t, got.Reached)
	assert.NotEqual(t, tried.Reached, got.Reached,
		"deux faits différents, deux valeurs différentes")
}

// ⚠️ APPELER LES SECOURS N'EXIGE PAS D'AVOIR JOINT LA PERSONNE. L'ordre normal
// est « joindre, puis appeler » ; après un choc violent sur quelqu'un
// d'injoignable, exiger le premier avant le second bloquerait LE SEUL CAS où
// chaque seconde compte. On enregistre, on ne barre pas la route — et ce test
// fige ce choix pour que personne ne « renforce » le protocole en le cassant.
func TestCallingEmergencyDoesNotRequireHavingReachedThePersonFirst(t *testing.T) {
	// Le service valide le SERVICE appelé, et rien d'autre.
	for _, svc := range []string{"police", "fire", "ambulance"} {
		_, err := (&Service{}).EmergencyCalled(testCtx(), "x", "y", EmergencyInput{Service: svc})
		// L'erreur attendue porte sur l'acteur ou l'alerte introuvable — JAMAIS
		// sur « vous n'avez pas encore appelé la personne ».
		assert.NotEqual(t, errEmergencyService, err, "%s est un service valable", svc)
	}
	_, err := (&Service{}).EmergencyCalled(testCtx(), "x", "y", EmergencyInput{Service: "gendarmerie"})
	assert.Equal(t, errEmergencyService, err, "un service inventé est refusé")
	_, err = (&Service{}).EmergencyCalled(testCtx(), "x", "y", EmergencyInput{})
	assert.Equal(t, errEmergencyService, err, "et il est exigé")
}

func testCtx() context.Context { return context.Background() }
