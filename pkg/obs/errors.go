package obs

// LES PANNES — capturées, pas seulement journalisées.
//
// ⚠️ UNE PILE D'EXÉCUTION PERDUE DANS UN FLUX DE TEXTE N'A JAMAIS RÉPARÉ
// PERSONNE. Une panique écrite dans les journaux d'un conteneur a trois
// défauts : elle disparaît à la rotation, personne ne la voit passer, et la
// même panique répétée mille fois ressemble à mille problèmes. Capturée,
// elle est GROUPÉE — « cette erreur, 412 fois depuis mardi, toujours au même
// endroit » —, datée, comptée, et elle attend qu'on la regarde.
//
// ⚠️ ELLE N'APPELLE PAS DE SERVICE EXTÉRIEUR. La plateforme a déjà de quoi
// stocker, notifier et afficher : ajouter un Sentry auto-hébergé coûterait
// plus de mémoire que tout le reste de la supervision réunie, et un Sentry
// hébergé ferait sortir les traces d'exécution — où figurent des
// identifiants de comptes et des morceaux de requêtes.
//
// ⚠️ ON NE CAPTURE PAS TOUT. Un `404` n'est pas une panne, un `422` non plus :
// ce sont des refus, ils sont attendus et déjà comptés par les mesures.
// Ici ne remontent que ce qui n'aurait pas dû arriver — les paniques et les
// `5xx`.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Fault est une panne telle qu'on la range.
type Fault struct {
	// Fingerprint GROUPE les occurrences d'un même défaut. ⚠️ Calculée sur ce
	// qui ne varie pas — le service, le type, l'endroit — et jamais sur le
	// message complet : « course 6ab8… introuvable » et « course 6ac1…
	// introuvable » sont le MÊME défaut, et les séparer rendrait la liste
	// illisible au bout d'une heure.
	Fingerprint string
	Service     string
	Version     string
	Kind        string // panic | http_5xx
	Message     string
	Stack       string
	Route       string
	Method      string
	Status      int
	RequestID   string
	Country     string
	UserID      string
	At          time.Time
}

// Sink range une panne. Implémenté au câblage — la base du socle, ou rien du
// tout en développement.
//
// ⚠️ IL NE DOIT JAMAIS FAIRE ÉCHOUER LA REQUÊTE. Une supervision qui casse ce
// qu'elle observe est pire que pas de supervision : on la coupe, et on se
// retrouve aveugle pour de bon.
type Sink interface {
	Capture(ctx context.Context, f Fault)
}

// SetSink câble le rangement des pannes (câblage). Sans lui, elles ne sont
// que journalisées — le comportement d'avant.
func (m *Metrics) SetSink(s Sink) { m.sink = s }

// Capture range une panne, si un rangement est câblé.
func (m *Metrics) Capture(ctx context.Context, f Fault) {
	if m == nil || m.sink == nil {
		return
	}
	f.Service = m.service
	if f.At.IsZero() {
		f.At = time.Now().UTC()
	}
	f.RequestID, f.Country, f.UserID = Field(ctx, KeyRequest), Field(ctx, KeyCountry), Field(ctx, KeyUser)
	if f.Route == "" {
		f.Route = Field(ctx, KeyRoute)
	}
	f.Fingerprint = fingerprint(f)
	m.sink.Capture(ctx, f)
}

// fingerprint construit la clé de groupement.
//
// ⚠️ LE MESSAGE EST NORMALISÉ AVANT D'ENTRER : les identifiants, les nombres
// et les durées en sont retirés. Sans cela, chaque occurrence aurait sa propre
// empreinte, et « grouper » ne voudrait plus rien dire.
func fingerprint(f Fault) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|%s", f.Service, f.Kind, f.Method, f.Route, normalise(f.Message))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// normalise remplace ce qui varie d'une occurrence à l'autre.
func normalise(msg string) string {
	var b strings.Builder
	digits := 0
	for _, r := range msg {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if isHex {
			digits++
			continue
		}
		// Une suite d'au moins quatre caractères hexadécimaux est un
		// identifiant, une durée ou un compteur : on la remplace par un
		// repère. En dessous, c'est un mot ordinaire — « b » de « base »,
		// « 5 » de « 5xx ».
		if digits > 0 {
			if digits >= 4 {
				b.WriteString("·")
			} else {
				b.WriteString(strings.Repeat("0", digits))
			}
			digits = 0
		}
		b.WriteRune(r)
	}
	if digits >= 4 {
		b.WriteString("·")
	}
	return b.String()
}

// Faulter est ce que `httpx` attend pour capturer un `5xx` : le même geste que
// `Metrics.Capture`, vu comme une interface pour que le paquet des réponses
// HTTP n'ait pas à connaître le registre de mesures.
type Faulter interface {
	Capture(ctx context.Context, f Fault)
}
