package compliance

// LA COLLECTE DES PIÈCES MANQUANTES — relancer les gens, sans les harceler.
//
// ⚠️ POURQUOI CECI EXISTE MAINTENANT. Cinq types de pièces ont été ajoutés le
// même jour (casier judiciaire, selfie, trois photos du véhicule), puis une
// sixième pour les vélos. Du jour au lendemain, TOUS les chauffeurs et livreurs
// déjà inscrits ont eu des pièces manquantes — sans que personne ne leur ait
// jamais demandé de les envoyer. Un écran qui dit « pas en règle » à quelqu'un
// qui n'a rien fait de mal, et qui ne lui dit pas QUOI envoyer, ne produit que
// des appels au support.
//
// ⚠️ CE N'EST PAS UN BLOCAGE, ET IL NE FAUT PAS LE PRÉSENTER COMME TEL. Rien,
// dans aucune des deux verticales, ne conditionne le dispatch à `compliant` —
// on l'a vérifié. « Il vous manque une pièce » est donc une liste de choses à
// envoyer, et le message le dit ainsi. Un message qui menacerait d'une
// suspension qui n'arrive pas use sa propre crédibilité.
//
// ⚠️ ET LA RELANCE EST LANCÉE PAR QUELQU'UN, pas par un minuteur. Une campagne
// qui part toute seule chaque matin devient un fond sonore : la personne apprend
// à balayer la bannière, et c'est la relance suivante — celle qui compte — qui
// ne sera pas lue. L'exploitation décide quand relancer, voit d'abord QUI serait
// relancé (`MissingSweep`), et le serveur refuse de redire la même chose à la
// même personne avant `RemindEvery`.

import (
	"context"
	"sort"
	"strings"
	"time"
)

// RemindEvery : on ne redit pas à la même personne qu'il lui manque une pièce
// avant ce délai.
//
// ⚠️ TROIS JOURS, et c'est un compromis entre deux façons de rater. Plus court,
// la relance devient du bruit — or elle n'est PAS coupable (catégorie
// `support`), donc la personne ne peut pas la couper : elle apprendrait
// simplement à ne plus la lire. Plus long, quelqu'un qui a changé de téléphone
// entre deux envois ne la voit jamais.
const RemindEvery = 72 * time.Hour

// Reminder pousse un message à une personne, au plus une fois par fenêtre.
//
// Déclaré ici plutôt qu'importé : ce paquet est une bibliothèque partagée par
// les deux verticales, et il ne doit pas connaître le module de notification du
// socle.
type Reminder interface {
	// NotifyOnce rend `true` si le message est parti.
	NotifyOnce(ctx context.Context, userID, key string, within time.Duration, vars, data map[string]string) bool
}

// SetReminder branche la relance (câblage). Sans elle, `Remind` ne fait rien et
// le dit.
func (s *Service) SetReminder(r Reminder) { s.remind = r }

// KeyDocumentsMissing est la clé du message de relance.
const KeyDocumentsMissing = "documents_missing"

// MissingPerson est quelqu'un à qui il manque des pièces.
type MissingPerson struct {
	DriverID string `json:"driver_id"`
	// UserID est le COMPTE. ⚠️ Vide, on ne peut PAS relancer : il n'y a pas
	// d'appareil à qui pousser. La personne apparaît quand même dans la liste,
	// marquée — un profil sans compte lié est un défaut d'exploitation qu'il
	// vaut mieux voir que cacher.
	UserID string `json:"user_id,omitempty"`
	// Missing porte les clés telles que `Compliance` les rend —
	// `criminal_record`, `insurance:<vehicleID>`.
	Missing []string `json:"missing"`
	// Labels : les mêmes, en clair et dans la langue de l'exploitation. ⚠️
	// SERVIS, parce qu'un écran qui afficherait `vehicle_side:6ac8…` ferait
	// chercher à l'opérateur ce qu'il doit demander.
	Labels []string `json:"labels"`
}

// MissingSweep rend les gens à qui il manque des pièces — ce que
// l'exploitation voit AVANT de lancer une relance.
//
// ⚠️ ELLE PARCOURT LES CHAUFFEURS, et non les documents : les gens qui n'ont
// RIEN envoyé n'ont aucun document, et une liste bâtie sur les documents les
// aurait tous ratés — c'est-à-dire exactement ceux qu'il faut relancer.
//
// ⚠️ ET ELLE EST BORNÉE. Un balayage sans limite sur un parc de dix mille
// chauffeurs fait dix mille lectures de conformité dans une requête HTTP. La
// limite est le nombre de PERSONNES examinées, pas de personnes rendues : la
// console repagine avec `after`.
func (s *Service) MissingSweep(ctx context.Context, after string, limit int) ([]MissingPerson, string, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	ids, err := s.fleet.DriverIDs(ctx, after, limit)
	if err != nil {
		return nil, "", err
	}
	out := make([]MissingPerson, 0, len(ids))
	for _, driverID := range ids {
		st, err := s.complianceOf(ctx, driverID)
		if err != nil {
			// AU MIEUX : un chauffeur illisible ne doit pas faire échouer le
			// balayage. Le sauter le laisse hors de la relance, ce qui est un
			// manque ; faire échouer la liste la laisse vide, ce qui est pire.
			continue
		}
		if st.Compliant || len(st.Missing) == 0 {
			continue
		}
		userID, _ := s.fleet.AccountOf(ctx, driverID)
		out = append(out, MissingPerson{
			DriverID: driverID, UserID: userID,
			Missing: st.Missing, Labels: MissingLabels(st.Missing, "fr"),
		})
	}
	next := ""
	if len(ids) == limit {
		next = ids[len(ids)-1]
	}
	return out, next, nil
}

// MissingLabels nomme des pièces manquantes en clair, SANS les identifiants de
// véhicule.
//
// ⚠️ LE VÉHICULE EST RETIRÉ DU LIBELLÉ, pas gardé. « Assurance
// (6ac81db2fdb04195d7c49c29) » n'aide personne : ni l'opérateur, qui ne retient
// pas un hexadécimal, ni le livreur, qui n'en a qu'un. Et quand il y en a
// plusieurs, c'est la FICHE qui le dit — pas un message poussé de deux lignes.
//
// ⚠️ ET LES DOUBLONS SONT FONDUS : trois motos sans assurance font une ligne
// « Assurance », pas trois identiques.
func MissingLabels(missing []string, locale string) []string {
	seen := make(map[string]bool, len(missing))
	out := make([]string, 0, len(missing))
	for _, m := range missing {
		kind := m
		if i := strings.IndexByte(m, ':'); i >= 0 {
			kind = m[:i]
		}
		label := Label(kind, locale)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// RemindResult dit ce qu'une relance a fait.
type RemindResult struct {
	// Examined : combien de personnes ont été regardées.
	Examined int `json:"examined"`
	// Missing : combien d'entre elles ont des pièces manquantes.
	Missing int `json:"missing"`
	// Reminded : combien ont VRAIMENT reçu le message.
	Reminded int `json:"reminded"`
	// Skipped : déjà relancées dans la fenêtre. ⚠️ RENDU, parce que sans lui
	// une relance qui ne part pas ressemble à une panne — alors que c'est le
	// garde-fou qui fonctionne.
	Skipped int `json:"skipped"`
	// Unreachable : pas de compte lié, donc aucun appareil à prévenir.
	Unreachable int    `json:"unreachable"`
	NextAfter   string `json:"next_after,omitempty"`
}

// Remind relance les gens à qui il manque des pièces.
//
// ⚠️ LE MESSAGE NOMME LES PIÈCES. « Vous n'êtes pas en règle » n'appelle aucun
// geste ; « il vous manque votre casier judiciaire et une photo de votre
// véhicule » en appelle un. C'est toute la différence entre une relance et un
// reproche.
func (s *Service) Remind(ctx context.Context, after string, limit int) (*RemindResult, error) {
	people, next, err := s.MissingSweep(ctx, after, limit)
	if err != nil {
		return nil, err
	}
	res := &RemindResult{Missing: len(people), NextAfter: next}
	// `Examined` compte les personnes PARCOURUES, pas celles en défaut : c'est
	// ce qui permet de savoir où en est la pagination.
	res.Examined = limit
	if next == "" {
		res.Examined = len(people)
	}
	for _, p := range people {
		if p.UserID == "" {
			res.Unreachable++
			continue
		}
		if s.remind == nil {
			// Sans relance branchée, on ne prétend pas avoir envoyé.
			res.Unreachable++
			continue
		}
		sent := s.remind.NotifyOnce(ctx, p.UserID, KeyDocumentsMissing, RemindEvery,
			map[string]string{
				"count": itoa(len(p.Labels)),
				// ⚠️ TROIS AU PLUS DANS LE MESSAGE. Une bannière de téléphone
				// coupe au-delà, et une liste tronquée au milieu d'un mot est
				// pire qu'une liste courte suivie de « et 2 autres ».
				"documents": joinUpTo(p.Labels, 3),
			},
			map[string]string{"type": "documents_missing", "driver_id": p.DriverID})
		if sent {
			res.Reminded++
		} else {
			res.Skipped++
		}
	}
	return res, nil
}

func joinUpTo(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + " et " + itoa(len(items)-n) + " autre(s)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
