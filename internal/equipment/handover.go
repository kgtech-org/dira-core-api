package equipment

// LA REMISE PROUVÉE PAR UN SCAN.
//
// ⚠️ LE DÉFAUT QUE CECI CORRIGE : jusqu'ici la remise était enregistrée par
// l'EXPLOITATION seule (`POST /admin/equipment/contracts/{id}/hand-over`).
// Personne ne pouvait distinguer « le gilet a été remis » de « quelqu'un a
// cliqué sur Remettre » — et la différence n'est pas théorique : la remise
// DÉMARRE L'ÉCHÉANCIER. Un clic de trop, et un livreur rembourse pendant trois
// mois un sac qu'il n'a jamais eu. Il le dira, et nous n'aurions rien à opposer.
//
// Le scan ajoute le geste que SEUL LE PORTEUR peut faire : le comptoir affiche
// un code, le porteur le scanne avec son application. Les deux étaient donc au
// même endroit au même moment, et c'est lui qui a conclu.
//
// ⚠️ ET LE SCAN N'EST PAS OBLIGATOIRE — c'est la décision importante de ce
// fichier. Un comptoir sans réseau, un téléphone sans caméra, un écran cassé :
// si le scan était le seul chemin, la remise deviendrait impossible et
// l'exploitation s'arrêterait. La voie de l'exploitation reste donc ouverte, et
// le contrat garde COMMENT il a été conclu (`HandedVia`). C'est ce champ qu'on
// regarde quand quelqu'un conteste, et il vaut mieux savoir que la preuve
// manque que croire qu'elle existe.

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Comment une remise a été conclue.
const (
	// HandedViaStaff : l'exploitation l'a enregistrée. Aucune preuve du côté du
	// porteur — c'était le seul chemin avant le scan, et il reste le repli.
	HandedViaStaff = "staff"
	// HandedViaScan : le porteur a scanné le code du comptoir. Il était là, et
	// c'est lui qui a conclu.
	HandedViaScan = "scan"
)

const (
	// handoverCodeTTL : la durée de vie d'un code.
	//
	// ⚠️ COURTE, ET C'EST TOUT L'INTÉRÊT. Un code qui vivrait la journée se
	// photographie au comptoir et se scanne le soir, de chez soi : la preuve
	// « nous étions au même endroit au même moment » disparaît, et il ne reste
	// qu'un bouton Accepter avec une étape de plus. Cinq minutes laissent le
	// temps de sortir le téléphone, de déverrouiller, et d'échouer une fois.
	handoverCodeTTL = 5 * time.Minute
	// handoverCodeLen : 16 caractères sur un alphabet de 31, soit près de 80
	// bits. ⚠️ Un code court serait DEVINABLE, et deviner un code revient à se
	// faire remettre le matériel de quelqu'un d'autre — sur une fenêtre de cinq
	// minutes, mais répétée à chaque comptoir de la journée.
	handoverCodeLen = 16
)

// handoverAlphabet : base32 SANS les caractères qu'on confond.
//
// ⚠️ Même raisonnement que pour les codes promo : un code se lit parfois à voix
// haute quand la caméra ne veut pas, et « 0 » contre « O » fait échouer un
// comptoir qui ne comprend pas pourquoi.
const handoverAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// HandoverCode est le laissez-passer d'une remise, affiché en QR par la console.
type HandoverCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
	// ⚠️ `url` EST UN LIEN PROFOND, et le QR porte CE lien plutôt que le code
	// nu. Un QR qui ne contient qu'une suite de lettres n'est lisible que par
	// l'application déjà ouverte au bon écran ; avec un lien, l'appareil photo du
	// téléphone propose d'ouvrir Dira — et le porteur qui a fermé l'application
	// n'a pas à retrouver le menu pendant que le comptoir attend.
	URL string `json:"url"`
	// Contract et Item : de quoi écrire « Gilet M · Awa Ndiaye » SOUS le QR.
	//
	// ⚠️ Sans cela, le comptoir montre un carré noir et ne peut pas vérifier
	// qu'il affiche le bon contrat. Deux agents au comptoir, deux écrans
	// ouverts, et c'est la remise de l'autre qu'on fait conclure.
	ContractID string `json:"contract_id"`
	ItemName   string `json:"item_name"`
	HolderName string `json:"holder_name,omitempty"`
}

var (
	errHandoverCodeUnknown = apperr.NotFound("equipment_handover_code_unknown",
		"this code does not exist or has already been used")
	errHandoverCodeExpired = apperr.Conflict("equipment_handover_code_expired",
		"this code has expired: ask the counter for a new one")
	// ⚠️ NOMMÉ À PART d'un refus d'accès : scanner le code de quelqu'un d'autre
	// arrive pour de bon — deux porteurs au comptoir, deux écrans. Le message
	// doit dire « ce n'est pas votre matériel », pas « interdit ».
	errHandoverNotYours = apperr.Forbidden("equipment_handover_not_yours",
		"this hand-over belongs to someone else")
)

// HandoverLinkBase est la base des liens profonds du QR. Réglable parce qu'elle
// diffère entre la recette et la production.
func (s *Service) SetHandoverLinkBase(base string) { s.handoverBase = strings.TrimRight(base, "/") }

// MintHandoverCode prépare une remise : l'exploitation l'appelle, la console
// affiche le QR.
//
// ⚠️ IL REMPLACE LE PRÉCÉDENT. Un comptoir qui rafraîchit son écran ne doit pas
// laisser derrière lui une collection de codes valides pour le même contrat :
// chacun serait une remise possible, et il n'en faut qu'une.
func (s *Service) MintHandoverCode(ctx context.Context, actorID, id string) (*HandoverCode, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	// ⚠️ ON VÉRIFIE LA TRANSITION MAINTENANT, pas au scan. Afficher un QR pour
	// un contrat déjà actif ferait scanner le porteur pour rien — et c'est au
	// comptoir, devant lui, qu'il faut l'apprendre.
	if c.Status != StatusDraft && c.Status != StatusAccepted {
		return nil, errBadTransition
	}
	code, err := drawHandoverCode()
	if err != nil {
		return nil, apperr.Internal(err)
	}
	expires := s.now().Add(handoverCodeTTL)
	if err := s.repo.SetCode(ctx, c.ID, CodeHandover, code, expires, nil); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.handover_code", c, nil)
	out := &HandoverCode{
		Code: code, ExpiresAt: expires,
		URL:        s.handoverURL(code),
		ContractID: c.ID.Hex(), ItemName: c.ItemName,
	}
	// AU MIEUX : un nom manquant ne doit pas empêcher d'afficher le QR. Mais
	// quand il est là, il vaut la ligne — c'est ce qui permet au comptoir de
	// vérifier qu'il montre le bon écran.
	if s.accounts != nil {
		if names, err := s.accounts.UserNames(ctx, []string{c.UserID.Hex()}); err == nil {
			out.HolderName = names[c.UserID.Hex()]
		}
	}
	return out, nil
}

// ScanHandover : le porteur a scanné le code du comptoir.
//
// ⚠️ UN SEUL APPEL POUR ACCEPTER ET CONCLURE. L'acceptation des conditions et la
// remise étaient deux gestes à deux endroits (un bouton dans l'application, un
// clic au comptoir) ; le scan les réunit parce que, physiquement, c'est UN
// moment. Les garder séparés aurait obligé le porteur à accepter d'abord dans un
// autre écran — et le comptoir à attendre qu'il le trouve.
func (s *Service) ScanHandover(ctx context.Context, userID, raw string) (*ContractResponse, error) {
	code := NormaliseHandoverCode(raw)
	if code == "" {
		return nil, errHandoverCodeUnknown
	}
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errHandoverCodeUnknown
	}
	// ⚠️ LE CODE EST CONSOMMÉ D'ABORD, EN UNE SEULE ÉCRITURE — et seulement s'il
	// est bien le SIEN et encore valide. C'est ce qui rend deux scans simultanés
	// inoffensifs : le second ne trouve plus rien. Voir
	// `Repository.ConsumeCode`.
	c, err := s.repo.ConsumeCode(ctx, CodeHandover, code, oid, s.now())
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if c == nil {
		// Rien n'a été consommé : il faut dire POURQUOI, et une lecture sans
		// effet secondaire est le seul moyen honnête de le savoir.
		return nil, s.diagnoseHandover(ctx, code, userID)
	}
	if c.Status != StatusDraft && c.Status != StatusAccepted {
		// ⚠️ LE CODE EST DÉJÀ CONSOMMÉ ICI, ET ON NE LE REND PAS. Un contrat
		// qui n'est plus à remettre ne le redeviendra pas : garder le code
		// vivant pour « réessayer » n'aiderait personne et laisserait un secret
		// scannable derrière une remise déjà faite.
		if c.Status == StatusActive {
			// Déjà conclue — un double appui, ou une remise enregistrée au
			// comptoir entre-temps. On rend le contrat sans rien refaire.
			out := s.responses(ctx, []Contract{*c}, false)
			return &out[0], nil
		}
		return nil, errBadTransition
	}

	now := s.now()
	// L'ACCEPTATION, si elle n'avait pas déjà eu lieu dans l'application.
	if c.Status == StatusDraft {
		c.Status = StatusAccepted
		c.AcceptedAt = &now
	}
	// L'objet en mémoire porte encore le code lu AVANT la consommation : on le
	// vide pour que `SaveContract`, qui remplace le document entier, ne le
	// réécrive pas.
	c.HandoverCode = ""
	c.HandoverCodeExpiresAt = nil
	c.HandedVia = HandedViaScan
	c.HandedScanAt = &now

	return s.handOver(ctx, userID, c)
}

// diagnoseHandover dit pourquoi un scan n'a rien consommé.
//
// ⚠️ TROIS REFUS DISTINCTS, PARCE QU'ILS APPELLENT TROIS GESTES DIFFÉRENTS :
// demander un nouveau code au comptoir, regarder le bon écran, ou ne rien faire
// du tout. Un seul message « code invalide » les enverrait tous les trois au
// comptoir, qui ne saurait pas lequel il a devant lui.
func (s *Service) diagnoseHandover(ctx context.Context, code, userID string) error {
	c, err := s.repo.ContractByCode(ctx, CodeHandover, code)
	if err != nil {
		return apperr.Internal(err)
	}
	if c == nil {
		return errHandoverCodeUnknown
	}
	// ⚠️ LE PORTEUR D'ABORD, L'EXPIRATION ENSUITE. Dire « code expiré » à
	// quelqu'un qui a scanné le QR de son voisin l'enverrait réclamer un nouveau
	// code, alors que la seule chose à faire est de regarder le bon écran.
	if c.UserID.Hex() != userID {
		return errHandoverNotYours
	}
	if c.HandoverCodeExpiresAt == nil || !c.HandoverCodeExpiresAt.After(s.now()) {
		return errHandoverCodeExpired
	}
	// Le code est le sien, encore valide, et pourtant rien n'a été consommé :
	// une autre requête l'a pris entre les deux. C'est un double appui.
	return errHandoverCodeUnknown
}

// NormaliseHandoverCode accepte ce qu'un scanner rend vraiment.
//
// ⚠️ LE PAYLOAD PEUT ÊTRE UNE URL, parce que c'est ce que le QR porte. Une
// application qui enverrait le lien entier n'a pas tort — c'est ce que sa
// bibliothèque de scan lui a donné —, et refuser au motif que « ce n'est pas un
// code » aurait fait chercher une heure à quelqu'un pour un slash.
func NormaliseHandoverCode(raw string) string {
	s := strings.TrimSpace(raw)
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i]
	}
	s = strings.ToUpper(strings.TrimSpace(s))
	// Les tirets qu'on ajoute pour la lisibilité à voix haute ne font pas
	// partie du code.
	s = strings.ReplaceAll(s, "-", "")
	for _, r := range s {
		if !strings.ContainsRune(handoverAlphabet, r) {
			return ""
		}
	}
	return s
}

func (s *Service) handoverURL(code string) string {
	base := s.handoverBase
	if base == "" {
		// ⚠️ UN LIEN RELATIF PLUTÔT QU'UN DOMAINE DEVINÉ. Fabriquer
		// `https://dira.llc/...` quand rien n'est réglé enverrait les porteurs
		// de la recette sur la production — et le QR a l'air de marcher.
		return "/equipment/handover/" + code
	}
	return base + "/equipment/handover/" + code
}

// drawHandoverCode tire un code de 16 caractères.
//
// ⚠️ PAR REJET, ET NON PAR DÉCOUPAGE EN 5 BITS. J'ai écrit la seconde version
// d'abord : l'alphabet compte 31 caractères — 36 moins O, 0, I, 1 et L —, et un
// groupe de 5 bits vaut 0 à 31. L'indice 31 sortait du tableau, et comme un code
// porte seize groupes, environ QUATRE CODES SUR DIX auraient paniqué. Le test
// l'a trouvé au premier appel ; en production, c'est un comptoir sur deux qui
// n'aurait pas pu afficher son QR.
//
// ⚠️ ET LE REJET PLUTÔT QU'UN MODULO SEC : 256 n'est pas un multiple de 31, donc
// `b % 31` rendrait les huit premiers caractères légèrement plus fréquents. Sans
// conséquence pratique ici, mais un code est un secret, et un secret ne se tire
// pas « à peu près » uniformément quand l'écrire juste coûte deux lignes.
func drawHandoverCode() (string, error) {
	const n = len(handoverAlphabet) // 31
	const limit = byte(256 / n * n) // 248 : au-delà, on rejette
	out := make([]byte, 0, handoverCodeLen)
	buf := make([]byte, handoverCodeLen)
	for len(out) < handoverCodeLen {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("equipment: draw handover code: %w", err)
		}
		for _, b := range buf {
			if b >= limit {
				continue
			}
			out = append(out, handoverAlphabet[int(b)%n])
			if len(out) == handoverCodeLen {
				break
			}
		}
	}
	return string(out), nil
}
