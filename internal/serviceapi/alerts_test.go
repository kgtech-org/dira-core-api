package serviceapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// La charge RÉELLE d'Alertmanager (webhook v4), recopiée telle qu'elle arrive.
//
// ⚠️ ELLE PORTE SEPT CLÉS QUE NOUS NE LISONS PAS. Le décodeur strict de la
// plateforme la refusait avec `422 unknown_field: receiver` — dix-sept
// tentatives de remise, puis l'abandon. Trouvé en déployant, pas en testant :
// d'où ce test, qui garde la charge entière et pas la part qui nous intéresse.
const alertmanagerV4 = `{
  "version": "4",
  "groupKey": "{}/{severity=\"critique\"}:{alertname=\"PlateformeInjoignableDeDehors\"}",
  "truncatedAlerts": 0,
  "status": "firing",
  "receiver": "plateforme",
  "groupLabels": {"alertname": "PlateformeInjoignableDeDehors"},
  "commonLabels": {"alertname": "PlateformeInjoignableDeDehors", "severity": "critique"},
  "commonAnnotations": {"summary": "Une adresse publique ne répond plus"},
  "externalURL": "http://dira-alertmanager:9093",
  "alerts": [
    {
      "status": "firing",
      "labels": {"alertname": "PlateformeInjoignableDeDehors", "severity": "critique", "service": "core"},
      "annotations": {"summary": "Une adresse publique ne répond plus", "description": "files.dira.llc"},
      "startsAt": "2026-09-28T10:44:18.635Z",
      "endsAt": "0001-01-01T00:00:00Z",
      "generatorURL": "http://dira-vmalert:8880/vmalert/alert?...",
      "fingerprint": "8f0e1b2c3d4e5f60"
    }
  ]
}`

func TestTheRealAlertmanagerPayloadIsAccepted(t *testing.T) {
	got, err := decodeAlert(post(alertmanagerV4))
	require.NoError(t, err, "la charge réelle d'Alertmanager doit passer")

	assert.Equal(t, "firing", got.Status)
	require.Len(t, got.Alerts, 1)
	assert.Equal(t, "PlateformeInjoignableDeDehors", got.Alerts[0].Labels["alertname"])
	assert.Equal(t, "critique", got.Alerts[0].Labels["severity"])
	assert.Equal(t, "files.dira.llc", got.Alerts[0].Annotations["description"])
}

// ⚠️ ET UNE CLÉ QUE LA VERSION SUIVANTE AJOUTERA PASSE AUSSI. Exiger la forme
// d'un logiciel qu'on ne versionne pas, c'est se condamner à ne plus recevoir
// d'alertes le jour de sa mise à jour — et à ne l'apprendre qu'en lisant le
// journal d'Alertmanager, le dernier endroit où on regarde quand on attend
// justement une alerte.
func TestAFieldWeHaveNeverSeenDoesNotBreakAlerting(t *testing.T) {
	body := `{"status":"firing","uneCleDeLAnneeProchaine":{"a":[1,2]},"alerts":[{"status":"firing","labels":{"alertname":"X"},"annotations":{},"quoiEncore":true}]}`
	got, err := decodeAlert(post(body))
	require.NoError(t, err)
	require.Len(t, got.Alerts, 1)
	assert.Equal(t, "X", got.Alerts[0].Labels["alertname"])
}

// Du JSON qui n'en est pas reste refusé : tolérer les clés en trop n'est pas
// tout accepter.
func TestRubbishIsStillRefused(t *testing.T) {
	_, err := decodeAlert(post("ceci n'est pas du JSON"))
	require.Error(t, err)
}

// ⚠️ ON CESSE D'EXIGER LA FORME, DONC ON BORNE LA TAILLE. Sans cela, un logiciel
// qui déraille — ou quoi que ce soit qui atteigne ce point interne — ferait lire
// au socle un corps sans fin.
func TestAnEndlessBodyIsRefused(t *testing.T) {
	huge := `{"status":"firing","alerts":[],"remplissage":"` + strings.Repeat("a", maxAlertBodyBytes+1) + `"}`
	_, err := decodeAlert(post(huge))
	require.Error(t, err)
}

// Le texte venu d'ailleurs est borné : une description d'alerte peut contenir la
// sortie entière d'une requête, et non bornée elle partirait telle quelle dans
// une notification poussée.
func TestTextFromElsewhereIsClipped(t *testing.T) {
	assert.Equal(t, "abc", clip("  abc  ", 10))
	assert.Equal(t, "abcde…", clip("abcdefghij", 5))
}

func post(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/internal/alerts", strings.NewReader(body))
}
