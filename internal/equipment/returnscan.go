package equipment

// LE RETOUR PROUVÉ PAR UN SCAN — le symétrique de la remise, et pas son miroir.
//
// ⚠️ LE DÉFAUT QUE CECI CORRIGE N'EST PAS LE MÊME QUE CELUI DE LA REMISE. À la
// remise, la question était « l'article a-t-il vraiment été remis ? », parce que
// la remise DÉMARRE l'échéancier. Au retour, la question est l'inverse et elle
// se pose du côté du PORTEUR : « j'ai rendu le sac le 3, pourquoi me prélève-t-on
// encore le 20 ? ». Aujourd'hui il n'a rien à opposer, et l'exploitation non
// plus : seule une date saisie par un agent dit quand l'article est revenu.
//
// ⚠️ ET C'EST POURQUOI LE SCAN NE PROUVE PAS SEULEMENT UNE PRÉSENCE : il porte
// l'ACCEPTATION DU CONSTAT. Le comptoir examine l'article, écrit l'état et les
// dégâts, et le QR affiche ce qu'il a écrit — « Gilet M · bon état · 0 F de
// dégâts · caution rendue 5 000 F ». Le porteur scanne ce constat-là. Un scan
// qui n'aurait prouvé qu'« il était là » aurait laissé entière la seule chose
// qui se conteste vraiment : le montant retenu sur la caution.
//
// Le constat est donc POSÉ avec le code, et APPLIQUÉ au scan. Entre les deux, il
// ne s'est rien passé sur le contrat : un code qui expire sans être scanné ne
// laisse aucune trace, et le comptoir recommence.
//
// ⚠️ ET LA VOIE DE L'EXPLOITATION RESTE OUVERTE
// (`POST /admin/equipment/contracts/{id}/return`), exactement comme pour la
// remise : un comptoir sans réseau, un téléphone sans caméra, un porteur qui
// renvoie son sac par un collègue. Le contrat garde COMMENT le retour a été
// conclu (`ReturnedVia`), et il vaut mieux savoir que la preuve manque que
// croire qu'elle existe.

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Les deux gestes qui se concluent par un scan. ⚠️ Ce sont les PRÉFIXES des
// champs en base (`handover_code`, `return_code`) : les changer renomme des
// colonnes.
const (
	CodeHandover = "handover"
	CodeReturn   = "return"
)

// Comment un retour a été conclu.
const (
	// ReturnedViaStaff : l'exploitation l'a enregistré. Aucune acceptation du
	// porteur — c'était le seul chemin avant le scan, et il reste le repli.
	ReturnedViaStaff = "staff"
	// ReturnedViaScan : le porteur a scanné le constat affiché au comptoir. Il
	// était là, et il a accepté ce qui serait retenu.
	ReturnedViaScan = "scan"
)

// ReturnProposal est le constat POSÉ au comptoir, en attente du scan.
//
// ⚠️ IL NE MODIFIE RIEN tant que personne n'a scanné. C'est la différence entre
// « voici ce que nous allons retenir » et « nous avons retenu » — et c'est toute
// la valeur du geste.
type ReturnProposal struct {
	Condition     string     `bson:"condition,omitempty"`
	DamageFeeXOF  int        `bson:"damage_fee_xof,omitempty"`
	RefundDeposit *bool      `bson:"refund_deposit,omitempty"`
	ByStaff       string     `bson:"by_staff,omitempty"`
	At            *time.Time `bson:"at,omitempty"`
}

// ReturnCode est le laissez-passer d'un retour, affiché en QR par la console.
type ReturnCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
	URL       string    `json:"url"`
	// Contract, Item, Holder : de quoi écrire la ligne SOUS le QR, pour que le
	// comptoir vérifie qu'il montre le bon écran.
	ContractID string `json:"contract_id"`
	ItemName   string `json:"item_name"`
	HolderName string `json:"holder_name,omitempty"`
	// ⚠️ LE CONSTAT EST RENDU AVEC LE CODE, et c'est ce que le comptoir affiche
	// À CÔTÉ du QR. Un QR sans ces trois nombres ferait scanner le porteur sans
	// qu'il sache ce qu'il accepte — ce qui vaut moins qu'un clic d'agent,
	// parce que ça en a l'air plus.
	Condition      string `json:"condition,omitempty"`
	DamageFeeXOF   int    `json:"damage_fee_xof"`
	RefundXOF      int    `json:"refund_xof"`
	RefundsDeposit bool   `json:"refunds_deposit"`
}

var (
	errReturnCodeUnknown = apperr.NotFound("equipment_return_code_unknown",
		"this code does not exist or has already been used")
	errReturnCodeExpired = apperr.Conflict("equipment_return_code_expired",
		"this code has expired: ask the counter for a new one")
	// ⚠️ NOMMÉ À PART d'un refus d'accès : scanner le code de quelqu'un d'autre
	// arrive pour de bon — deux porteurs au comptoir, deux écrans. Le message
	// doit dire « ce n'est pas votre matériel », pas « interdit ».
	errReturnNotYours = apperr.Forbidden("equipment_return_not_yours",
		"this return belongs to someone else")
)

// MintReturnCode prépare un retour : le comptoir constate, la console affiche
// le QR.
func (s *Service) MintReturnCode(ctx context.Context, actorID, id string, req ReturnInput) (*ReturnCode, error) {
	c, err := s.load(ctx, id)
	if err != nil {
		return nil, err
	}
	// ⚠️ ON VÉRIFIE LA TRANSITION MAINTENANT, pas au scan. Afficher un QR pour
	// un contrat déjà rendu ferait scanner le porteur pour rien — et c'est au
	// comptoir, devant lui, qu'il faut l'apprendre.
	if c.Status != StatusActive {
		return nil, errBadTransition
	}
	code, err := drawHandoverCode()
	if err != nil {
		return nil, apperr.Internal(err)
	}
	now := s.now()
	expires := now.Add(handoverCodeTTL)
	proposal := ReturnProposal{
		Condition:     strings.TrimSpace(req.Condition),
		DamageFeeXOF:  req.DamageFeeXOF,
		RefundDeposit: req.RefundDeposit,
		ByStaff:       actorID,
		At:            &now,
	}
	if err := s.repo.SetCode(ctx, c.ID, CodeReturn, code, expires,
		bson.M{"return_proposal": proposal}); err != nil {
		return nil, err
	}
	s.record(ctx, "equipment.return_code", c, nil)

	refunds, refund := previewRefund(c, proposal)
	out := &ReturnCode{
		Code: code, ExpiresAt: expires,
		URL:        s.returnURL(code),
		ContractID: c.ID.Hex(), ItemName: c.ItemName,
		Condition: proposal.Condition, DamageFeeXOF: proposal.DamageFeeXOF,
		RefundXOF: refund, RefundsDeposit: refunds,
	}
	// AU MIEUX : un nom manquant ne doit pas empêcher d'afficher le QR.
	if s.accounts != nil {
		if names, err := s.accounts.UserNames(ctx, []string{c.UserID.Hex()}); err == nil {
			out.HolderName = names[c.UserID.Hex()]
		}
	}
	return out, nil
}

// previewRefund calcule ce qui serait rendu, SANS rien écrire.
//
// ⚠️ LE MÊME CALCUL QUE `Return`, et c'est un risque assumé qu'il faut nommer :
// deux formules qui divergent feraient afficher 5 000 sous le QR et verser
// 4 000 — c'est-à-dire la réclamation que ce geste était censé éteindre. Le
// calcul est donc EXTRAIT, et `Return` l'appelle aussi ; un test les compare.
func previewRefund(c *Contract, p ReturnProposal) (refunds bool, refundXOF int) {
	refunds = c.Plan.DepositRefundable
	if p.RefundDeposit != nil {
		refunds = *p.RefundDeposit
	}
	if !refunds {
		return false, 0
	}
	paid := 0
	for _, l := range c.Schedule {
		if l.Kind == LineDeposit {
			paid += l.PaidXOF
		}
	}
	return true, max(paid-p.DamageFeeXOF, 0)
}

// ScanReturn : le porteur a scanné le constat du comptoir.
func (s *Service) ScanReturn(ctx context.Context, userID, raw string) (*ContractResponse, error) {
	code := NormaliseHandoverCode(raw)
	if code == "" {
		return nil, errReturnCodeUnknown
	}
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errReturnCodeUnknown
	}
	// ⚠️ LE CODE EST CONSOMMÉ D'ABORD, EN UNE SEULE ÉCRITURE — et seulement s'il
	// est bien le SIEN et encore valide. C'est ce qui rend deux scans simultanés
	// inoffensifs : le second ne trouve plus rien, donc pas deux rendus de
	// caution ni deux ajustements de stock.
	c, err := s.repo.ConsumeCode(ctx, CodeReturn, code, oid, s.now())
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if c == nil {
		return nil, s.diagnoseReturn(ctx, code, userID)
	}
	if c.Status != StatusActive {
		// ⚠️ LE CODE EST DÉJÀ CONSOMMÉ ICI, ET ON NE LE REND PAS. Un contrat qui
		// n'est plus à rendre ne le redeviendra pas.
		if c.Status == StatusReturned {
			// Déjà conclu — un double appui, ou un retour enregistré au comptoir
			// entre-temps. On rend le contrat sans rien refaire.
			out := s.responses(ctx, []Contract{*c}, false)
			return &out[0], nil
		}
		return nil, errBadTransition
	}
	// Le constat posé au comptoir est ce qu'on applique. ⚠️ Et on ne relit PAS
	// la requête du porteur : il accepte, il ne négocie pas.
	p := c.ReturnProposal
	return s.applyReturn(ctx, userID, c, ReturnInput{
		Condition:     p.Condition,
		DamageFeeXOF:  p.DamageFeeXOF,
		RefundDeposit: p.RefundDeposit,
	}, ReturnedViaScan)
}

// diagnoseReturn dit pourquoi un scan n'a rien consommé.
//
// ⚠️ TROIS REFUS DISTINCTS, comme pour la remise, parce qu'ils appellent trois
// gestes différents : demander un nouveau code, regarder le bon écran, ou ne
// rien faire du tout.
func (s *Service) diagnoseReturn(ctx context.Context, code, userID string) error {
	c, err := s.repo.ContractByCode(ctx, CodeReturn, code)
	if err != nil {
		return apperr.Internal(err)
	}
	if c == nil {
		return errReturnCodeUnknown
	}
	// ⚠️ LE PORTEUR D'ABORD, L'EXPIRATION ENSUITE. Dire « code expiré » à
	// quelqu'un qui a scanné le QR de son voisin l'enverrait réclamer un nouveau
	// code, alors que la seule chose à faire est de regarder le bon écran.
	if c.UserID.Hex() != userID {
		return errReturnNotYours
	}
	if c.ReturnCodeExpiresAt == nil || !c.ReturnCodeExpiresAt.After(s.now()) {
		return errReturnCodeExpired
	}
	return errReturnCodeUnknown
}

// returnURL est le lien profond du QR de retour.
func (s *Service) returnURL(code string) string {
	if s.handoverBase == "" {
		return "/equipment/return/" + code
	}
	return s.handoverBase + "/equipment/return/" + code
}
