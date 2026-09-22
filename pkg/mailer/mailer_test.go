package mailer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func at() time.Time { return time.Date(2026, 9, 22, 6, 0, 0, 0, time.UTC) }

func client() *SMTP {
	return New(Config{Host: "smtp.example.com", User: "report@dira.llc", Pass: "x",
		From: "report@dira.llc", FromName: "Dira"})
}

// ⚠️ UN CLIENT NON CONFIGURÉ REFUSE À VOIX HAUTE. Faire comme si le message
// était parti est pire que ne pas l'envoyer : personne ne va le chercher.
func TestAnUnconfiguredMailerRefusesInsteadOfPretending(t *testing.T) {
	m := New(Config{})
	assert.False(t, m.Configured())
	err := m.Send(context.Background(), Message{To: []string{"dev@dira.llc"}, Subject: "x", Text: "y"})
	assert.ErrorIs(t, err, ErrNotConfigured)
}

// Un envoi sans destinataire est une erreur de configuration, pas un
// non-événement : la taire la ferait durer des mois.
func TestSendingToNobodyIsAnError(t *testing.T) {
	err := client().Send(context.Background(), Message{To: []string{"", "  "}, Subject: "x", Text: "y"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no recipient")
}

// Une adresse invalide dans une liste ne prive pas les autres de leur
// rapport — elle est seulement écartée.
func TestOneBadAddressDoesNotSinkTheRest(t *testing.T) {
	got := cleanAddresses([]string{"dev@dira.llc", "pas une adresse", "", "ops@dira.llc", "DEV@dira.llc"})
	assert.Equal(t, []string{"dev@dira.llc", "ops@dira.llc"}, got, "les doublons tombent aussi, sans regard à la casse")
}

// Le sujet est encodé : « Rapport journalier — Lomé » perdrait ses accents
// et son tiret cadratin en ASCII brut.
func TestAnAccentedSubjectSurvives(t *testing.T) {
	raw, err := client().Build(Message{Subject: "Rapport journalier — Lomé", Text: "x"},
		[]string{"dev@dira.llc"}, at())
	require.NoError(t, err)
	head := string(raw)
	assert.Contains(t, head, "Subject: =?utf-8?q?")
	assert.NotContains(t, head, "Subject: Rapport journalier — Lomé")
}

// ⚠️ Un saut de ligne dans le sujet ajouterait des en-têtes — donc des
// destinataires cachés. Refusé, jamais nettoyé en silence.
func TestALineBreakInTheSubjectIsRefused(t *testing.T) {
	_, err := client().Build(Message{Subject: "Rapport\r\nBcc: voleur@ailleurs.com", Text: "x"},
		[]string{"dev@dira.llc"}, at())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line break")
}

// Le message porte TOUJOURS une part texte : une part HTML seule tombe dans
// les indésirables chez la moitié des fournisseurs.
func TestAnHTMLMessageAlsoCarriesItsText(t *testing.T) {
	raw, err := client().Build(Message{Subject: "Rapport", HTML: "<p>bonjour</p>"},
		[]string{"dev@dira.llc"}, at())
	require.NoError(t, err)
	s := string(raw)
	assert.Contains(t, s, "multipart/alternative")
	assert.Contains(t, s, "text/plain; charset=utf-8")
	assert.Contains(t, s, "text/html; charset=utf-8")
	// L'ordre compte : le lecteur affiche la DERNIÈRE part qu'il sait lire.
	assert.Less(t, strings.Index(s, "text/plain"), strings.Index(s, "text/html"))
}

// `Auto-Submitted` évite qu'une absence du bureau réponde au rapport et que
// la boîte d'envoi parte en boucle.
func TestAReportSaysItIsAutomatic(t *testing.T) {
	raw, err := client().Build(Message{Subject: "Rapport", Text: "x"}, []string{"dev@dira.llc"}, at())
	require.NoError(t, err)
	assert.Contains(t, string(raw), "Auto-Submitted: auto-generated")
}

// Un point seul en début de ligne termine un message SMTP. Il doit être
// doublé, sinon un rapport se coupe au milieu.
func TestALoneDotIsEscaped(t *testing.T) {
	raw, err := client().Build(Message{Subject: "Rapport", Text: "avant\n.\naprès"},
		[]string{"dev@dira.llc"}, at())
	require.NoError(t, err)
	assert.Contains(t, string(raw), "\r\n..\r\n")
}

// L'expéditeur porte son nom : « Dira <report@dira.llc> ».
func TestTheSenderIsNamed(t *testing.T) {
	raw, err := client().Build(Message{Subject: "Rapport", Text: "x"}, []string{"dev@dira.llc"}, at())
	require.NoError(t, err)
	assert.Contains(t, string(raw), `From: "Dira" <report@dira.llc>`)
}

// Sans adresse d'expédition explicite, c'est le compte qui expédie — et non
// une chaîne vide que le serveur refuserait.
func TestTheAccountIsTheDefaultSender(t *testing.T) {
	m := New(Config{Host: "smtp.example.com", User: "report@dira.llc"})
	assert.Equal(t, "report@dira.llc", m.From())
	assert.True(t, m.Configured())
}

// Le chiffrement n'a que deux valeurs, et une valeur inconnue retombe sur la
// plus sûre — jamais sur « aucun ».
func TestAnUnknownTLSModeFallsBackToImplicit(t *testing.T) {
	assert.Equal(t, TLSImplicit, New(Config{Host: "h", TLS: "none"}).cfg.TLS)
	assert.Equal(t, TLSImplicit, New(Config{Host: "h"}).cfg.TLS)
	assert.Equal(t, TLSStartTLS, New(Config{Host: "h", TLS: TLSStartTLS}).cfg.TLS)
	assert.Equal(t, 465, New(Config{Host: "h"}).cfg.Port)
}

func TestValidChecksTheSameWayEverywhere(t *testing.T) {
	assert.True(t, Valid("dev@dira.llc"))
	assert.True(t, Valid("  dev@dira.llc  "))
	assert.False(t, Valid("dev@"))
	assert.False(t, Valid(""))
}
