package promocode

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/audit"
	"github.com/kgtech-org/dira-core-api/pkg/country"
	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

// Les refus, NOMMÉS — et c'est tout l'enjeu de ce module.
//
// ⚠️ « CODE INVALIDE » EST LE PIRE DES MESSAGES. Quelqu'un qui saisit un code
// dont il a lu l'affiche veut savoir LAQUELLE des raisons s'applique : il s'est
// trompé d'un caractère, l'opération est finie, il l'a déjà utilisé, ou elle ne
// vaut pas sur ce qu'il commande. Un seul refus pour tout cela envoie tout le
// monde au support, qui n'en saura pas plus.
var (
	errCodeUnknown = apperr.NotFound("promo_code_unknown",
		"this code does not exist")
	errCodeExpired = apperr.Conflict("promo_code_expired",
		"this code is no longer valid")
	errCodeElsewhere = apperr.Conflict("promo_code_wrong_country",
		"this code is not valid in this country")
	errCodeWrongVertical = apperr.Conflict("promo_code_wrong_service",
		"this code does not apply to this kind of order")
	errCodeTooSmall = apperr.Conflict("promo_code_amount_too_low",
		"this order is below the minimum for this code")
	errCodeExhausted = apperr.Conflict("promo_code_exhausted",
		"this code has reached its limit")
	errCodeAlreadyUsed = apperr.Conflict("promo_code_already_used",
		"you have already used this code")
	errCodeOwnReferral = apperr.Conflict("promo_code_own_referral",
		"you cannot use your own referral code")
	errCodeInvalid = apperr.Validation(
		"a code is 4 to 24 letters and digits")
	errInfluencerExists = apperr.Conflict("influencer_exists",
		"this account is already an influencer")
)

// Service applies the platform's promo codes.
type Service struct {
	repo  *Repository
	audit *audit.Recorder
	// LE PARRAINAGE — voir `referral.go`. Les deux sont FACULTATIFS : sans
	// politique, le parrainage est éteint ; sans portefeuille, le parrain
	// n'est pas payé et le journal crie.
	referral ReferralReader
	credits  Credits
}

func NewService(repo *Repository, auditor *audit.Recorder) *Service {
	return &Service{repo: repo, audit: auditor}
}

// Grant est ce qu'un code accorde — ou ce qui l'en empêche.
type Grant struct {
	Code        string `json:"code"`
	Label       string `json:"label,omitempty"`
	Kind        string `json:"kind"`
	DiscountXOF int    `json:"discount_xof"`
	// OwnerID : l'influenceur ou le parrain, pour que la verticale puisse
	// l'inscrire sur l'opération — c'est ce qui rend l'attribution possible
	// plus tard, quand on voudra savoir d'où venait un client.
	OwnerID string `json:"owner_id,omitempty"`
}

// Quote dit ce qu'un code accorderait sur un montant, SANS rien réserver.
//
// ⚠️ SANS RÉSERVER, et c'est la distinction qui fait tout : un devis se
// recalcule à chaque frappe, à chaque changement d'adresse, à chaque retour
// sur l'écran. Réserver au devis aurait épuisé une enveloppe avec des gens qui
// regardent, et le code aurait été « épuisé » sans qu'une seule course ne soit
// partie.
func (s *Service) Quote(ctx context.Context, raw, userID, vertical string, amountXOF int) (*Grant, error) {
	code, c, err := s.resolve(ctx, raw, userID, vertical, amountXOF)
	if err != nil {
		return nil, err
	}
	return &Grant{
		Code: code.Code, Label: code.Label, Kind: code.Kind,
		DiscountXOF: c, OwnerID: ownerHex(code),
	}, nil
}

// Redeem réserve l'usage d'un code pour une opération.
//
// ⚠️ IDEMPOTENT PAR RÉFÉRENCE, et c'est le registre partagé qui le garantit :
// le couple (code, référence) est unique. Un rappel de paiement rejoué, un
// redémarrage au mauvais moment ou un double clic ne consomment pas
// l'enveloppe deux fois.
func (s *Service) Redeem(ctx context.Context, raw, userID, vertical, refID string, amountXOF int) (*Grant, error) {
	if refID == "" {
		return nil, apperr.Validation("a redemption needs the reference it applies to")
	}
	code, discount, err := s.resolve(ctx, raw, userID, vertical, amountXOF)
	if err != nil {
		return nil, err
	}
	use := promo.Use{
		// ⚠️ LE CODE LUI-MÊME COMME `promo_id`, et non son ObjectID. C'est lui
		// qu'on lit dans un suivi, qu'on tape dans une recherche et qu'on
		// montre à un influenceur : un identifiant hexadécimal aurait obligé
		// chaque lecture à une jointure, pour une clé déjà unique.
		PromoID: code.Code, UserID: userID, RefID: refID,
		Country: code.Country, AmountXOF: discount,
		State: promo.StateReserved, Title: code.Label,
	}
	if err := s.repo.Ledger().Reserve(ctx, use); err != nil {
		if err == promo.ErrAlreadyUsed {
			// Cette référence a déjà consommé ce code : on rend ce qui avait
			// été accordé plutôt qu'une erreur. L'appelant rejoue, et doit
			// retrouver le même prix.
			return &Grant{Code: code.Code, Label: code.Label, Kind: code.Kind,
				DiscountXOF: discount, OwnerID: ownerHex(code)}, nil
		}
		return nil, apperr.Internal(err)
	}
	slog.InfoContext(ctx, "promocode: reserved",
		"code", code.Code, "kind", code.Kind, "ref_id", refID, "discount_xof", discount)
	return &Grant{Code: code.Code, Label: code.Label, Kind: code.Kind,
		DiscountXOF: discount, OwnerID: ownerHex(code)}, nil
}

// Settle marque l'usage comme ABOUTI : l'argent est sorti.
//
// ⚠️ C'EST ICI QUE LE PARRAIN EST PAYÉ, et pas à la saisie du code. Récompenser
// à la saisie aurait payé des parrainages que personne n'a honorés : il suffit
// de commander puis d'annuler. Voir `referral.go`.
func (s *Service) Settle(ctx context.Context, refID string) error {
	// La ligne AVANT de la clore : une fois réglée, elle ne dira plus quel
	// code elle portait ni combien, et c'est ce qu'il faut pour payer.
	uses, err := s.repo.UsesOf(ctx, refID)
	if err != nil {
		return apperr.Internal(err)
	}
	if err := s.repo.Ledger().Settle(ctx, refID); err != nil {
		return apperr.Internal(err)
	}
	for _, u := range uses {
		if u.State == promo.StateReserved {
			s.rewardSponsor(ctx, u)
		}
	}
	return nil
}

// Release rend l'enveloppe : la course est annulée, la commande refusée.
//
// ⚠️ SANS CELA UNE ENVELOPPE FOND SUR DES OPÉRATIONS QUI N'ONT PAS EU LIEU, et
// l'offre s'arrête sans qu'un franc ne soit sorti. C'est le défaut qu'on ne
// voit jamais venir : le budget est « consommé », tout le monde cherche où est
// passé l'argent, et il n'est jamais parti.
func (s *Service) Release(ctx context.Context, refID string) error {
	if err := s.repo.Ledger().Release(ctx, refID); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

// resolve fait tout le travail de décision, et le fait UNE fois.
//
// ⚠️ L'ORDRE DES REFUS EST CELUI DE L'UTILITÉ, pas celui du code. On dit
// d'abord ce qui ne se rattrape pas (code inconnu, expiré, mauvais pays), puis
// ce que la personne peut changer (le montant), puis ce qui vient de nous
// (enveloppe épuisée). Un refus « enveloppe épuisée » sur un code mal tapé
// ferait chercher du côté de l'offre une faute de frappe.
func (s *Service) resolve(ctx context.Context, raw, userID, vertical string, amountXOF int) (*Code, int, error) {
	norm := Normalise(raw)
	if !ValidCode(norm) {
		return nil, 0, errCodeInvalid
	}
	code, err := s.repo.ByCode(ctx, norm)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	if code == nil {
		return nil, 0, errCodeUnknown
	}
	if !code.Live(time.Now().UTC()) {
		return nil, 0, errCodeExpired
	}
	// ⚠️ LE PAYS DE L'OPÉRATION, pas celui du compte : un Togolais qui
	// commande à Dakar relève du catalogue sénégalais, et un code togolais n'y
	// vaut rien. Le refus le DIT, au lieu de prétendre que le code n'existe
	// pas.
	if at := country.FromContext(ctx); at != "" && code.Country != "" && code.Country != at {
		return nil, 0, errCodeElsewhere.WithMeta(map[string]any{"valid_in": code.Country})
	}
	if !code.Serves(vertical) {
		return nil, 0, errCodeWrongVertical.WithMeta(map[string]any{"valid_for": code.Verticals})
	}
	// ⚠️ SON PROPRE PARRAINAGE EST REFUSÉ, et nommément. Sans ce test, le
	// premier geste de chacun serait de saisir son propre code — et
	// l'opération se paierait elle-même, à grande échelle, en une soirée.
	if code.Kind == KindReferral && code.OwnerID != nil && code.OwnerID.Hex() == userID {
		return nil, 0, errCodeOwnReferral
	}
	if amountXOF < code.MinAmountXOF {
		return nil, 0, errCodeTooSmall.WithMeta(map[string]any{"min_amount_xof": code.MinAmountXOF})
	}
	discount := code.DiscountOn(amountXOF)
	if discount <= 0 {
		return nil, 0, errCodeTooSmall.WithMeta(map[string]any{"min_amount_xof": code.MinAmountXOF})
	}

	// La limite PAR PERSONNE, lue dans le registre : c'est elle qui protège de
	// l'abus, pas le budget. Sans elle, une seule personne consomme
	// l'enveloppe entière — et elle le fera.
	userUses := -1
	if userID != "" {
		byCode, err := s.repo.Ledger().UsesByUser(ctx, userID)
		if err != nil {
			return nil, 0, apperr.Internal(err)
		}
		userUses = byCode[code.Code]
	}
	reason, ok := promo.Allows(code.Limits, code.Counters, userUses, discount)
	if !ok {
		switch reason {
		case promo.ReasonPerUser:
			return nil, 0, errCodeAlreadyUsed.WithMeta(map[string]any{
				"max_uses_per_user": code.MaxUsesPerUser,
			})
		case promo.ReasonNoWallet:
			// Une limite par personne sans personne connue : on refuse. Une
			// offre « une fois par personne » servie à un inconnu est une
			// offre sans limite.
			return nil, 0, errCodeAlreadyUsed
		default:
			return nil, 0, errCodeExhausted.WithMeta(map[string]any{"reason": reason})
		}
	}
	return code, discount, nil
}

func ownerHex(c *Code) string {
	if c.OwnerID == nil {
		return ""
	}
	return c.OwnerID.Hex()
}

// --- CRÉER DES CODES ------------------------------------------------------

// NewCodeRequest crée un code, à la main ou en le TIRANT.
type NewCodeRequest struct {
	// Code : laissé VIDE, le serveur en tire un. C'est le cas courant pour un
	// influenceur — personne ne veut inventer vingt mots uniques.
	Code  string `json:"code" validate:"omitempty,max=24"`
	Kind  string `json:"kind" validate:"required,oneof=campaign influencer referral"`
	Label string `json:"label" validate:"omitempty,max=80"`
	// OwnerID : l'influenceur (sa FICHE) ou le parrain (son compte).
	OwnerID string `json:"owner_id" validate:"omitempty,len=24,hexadecimal"`
	// Prefix : de quoi tirer un code lisible — « AWA » donne « AWA7K2M ».
	Prefix string `json:"prefix" validate:"omitempty,alphanum,max=10"`

	Verticals      []string `json:"verticals" validate:"omitempty,max=2,dive,oneof=vtc food"`
	DiscountKind   string   `json:"discount_kind" validate:"required,oneof=percent amount"`
	Value          int      `json:"value" validate:"required,min=1"`
	MaxDiscountXOF int      `json:"max_discount_xof" validate:"min=0"`
	MinAmountXOF   int      `json:"min_amount_xof" validate:"min=0"`

	BudgetXOF      int `json:"budget_xof" validate:"min=0"`
	MaxUses        int `json:"max_uses" validate:"min=0"`
	MaxUsesPerUser int `json:"max_uses_per_user" validate:"min=0"`

	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

// Create writes a code, drawing one when none was given.
func (s *Service) Create(ctx context.Context, req NewCodeRequest) (*Code, error) {
	if req.DiscountKind == KindPercent && (req.Value < 1 || req.Value > 100) {
		return nil, apperr.Validation("a percentage is between 1 and 100")
	}
	// ⚠️ UNE REMISE EN POURCENTAGE SANS PLAFOND EST UNE PORTE OUVERTE. 20 %
	// d'une location de 55 000 F font 11 000 F offerts sur UNE opération. On
	// ne l'interdit pas — une campagne peut l'assumer — mais on le DIT, et le
	// journal d'audit le gardera.
	if req.DiscountKind == KindPercent && req.MaxDiscountXOF == 0 {
		slog.WarnContext(ctx, "promocode: percentage code without a cap",
			"kind", req.Kind, "value", req.Value)
	}
	if req.EndsAt.IsZero() || !req.EndsAt.After(req.StartsAt) {
		return nil, apperr.Validation("ends_at must be after starts_at")
	}
	c := &Code{
		Kind: req.Kind, Label: req.Label, Country: country.FromContext(ctx),
		Verticals: req.Verticals, DiscountKind: req.DiscountKind, Value: req.Value,
		MaxDiscountXOF: req.MaxDiscountXOF, MinAmountXOF: req.MinAmountXOF,
		Limits: promo.Limits{
			BudgetXOF: req.BudgetXOF, MaxUses: req.MaxUses,
			MaxUsesPerUser: req.MaxUsesPerUser,
		},
		StartsAt: req.StartsAt, EndsAt: req.EndsAt, Active: true,
	}
	if req.OwnerID != "" {
		oid, err := primitive.ObjectIDFromHex(req.OwnerID)
		if err != nil {
			return nil, apperr.Validation("invalid owner id")
		}
		c.OwnerID = &oid
	}
	if req.Code != "" {
		c.Code = Normalise(req.Code)
		if !ValidCode(c.Code) {
			return nil, errCodeInvalid
		}
		if err := s.repo.Create(ctx, c); err != nil {
			if err == ErrDuplicateCode {
				return nil, apperr.Conflict("promo_code_taken", "this code is already taken")
			}
			return nil, apperr.Internal(err)
		}
	} else if err := s.draw(ctx, c, req.Prefix); err != nil {
		return nil, err
	}
	if s.audit != nil {
		s.audit.Record(ctx, "promocode.create", "promo_code", c.Code, nil, map[string]any{
			"kind": c.Kind, "discount_kind": c.DiscountKind, "value": c.Value,
			"budget_xof": c.BudgetXOF, "max_uses": c.MaxUses,
			"max_uses_per_user": c.MaxUsesPerUser, "cap_xof": c.MaxDiscountXOF,
		})
	}
	return c, nil
}

// draw tire un code libre, en réessayant sur collision.
//
// ⚠️ ON RÉESSAIE PLUTÔT QUE DE VÉRIFIER D'ABORD. Lire « ce code existe-t-il ? »
// puis écrire laisse une fenêtre entre les deux : deux créations simultanées
// passeraient le test et la seconde échouerait, en production, sur l'écran de
// quelqu'un. C'est l'index unique qui tranche, et la boucle qui s'adapte.
func (s *Service) draw(ctx context.Context, c *Code, prefix string) error {
	prefix = strings.ToUpper(prefix)
	for range 8 {
		c.Code = prefix + randomCode(8-min(len(prefix), 4))
		if !ValidCode(c.Code) {
			return errCodeInvalid
		}
		err := s.repo.Create(ctx, c)
		if err == nil {
			return nil
		}
		if err != ErrDuplicateCode {
			return apperr.Internal(err)
		}
	}
	// Huit collisions d'affilée sur un alphabet de 30 caractères : ce n'est
	// pas du hasard, c'est un préfixe saturé. Le dire plutôt que boucler.
	return apperr.Conflict("promo_code_undrawable",
		"could not draw a free code with this prefix: try a different one")
}

// drawAlphabet : les caractères d'un code TIRÉ.
//
// ⚠️ NI `O` NI `0`, NI `I` NI `1`, NI `L`. Un code se dicte au téléphone et se
// recopie depuis une affiche : chaque caractère ambigu est un code que
// quelqu'un n'arrivera pas à saisir, et un appel au support qui coûte plus que
// la remise.
const drawAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func randomCode(n int) string {
	if n < 4 {
		n = 4
	}
	out := make([]byte, n)
	for i := range out {
		// crypto/rand : un code devinable est un code que quelqu'un essaiera
		// en boucle, et l'enveloppe est à ce bout-là.
		k, err := rand.Int(rand.Reader, big.NewInt(int64(len(drawAlphabet))))
		if err != nil {
			// Un tirage impossible est une panne de la machine, pas un cas
			// métier : on ne fabrique pas un code faible en secours.
			panic(fmt.Sprintf("promocode: no randomness: %v", err))
		}
		out[i] = drawAlphabet[k.Int64()]
	}
	return string(out)
}

// SetActive allume ou éteint un code.
//
// ⚠️ ÉTEINDRE N'EFFACE PAS. Les usages restent, et c'est la seule façon de
// répondre à « combien cette campagne a-t-elle coûté ? » après l'avoir
// arrêtée. Un code supprimé emporterait sa facture avec lui.
func (s *Service) SetActive(ctx context.Context, code string, active bool) error {
	c, err := s.repo.ByCode(ctx, Normalise(code))
	if err != nil {
		return apperr.Internal(err)
	}
	if c == nil {
		return errCodeUnknown
	}
	if err := s.repo.Update(ctx, c.ID, bson.M{"active": active}); err != nil {
		return apperr.Internal(err)
	}
	if s.audit != nil {
		s.audit.Record(ctx, "promocode.set_active", "promo_code", c.Code, nil,
			map[string]any{"active": active})
	}
	return nil
}

// CreateInfluencer attache une fiche d'influenceur à un COMPTE existant.
//
// ⚠️ UNE FICHE PAR COMPTE. Deux fiches sur la même personne auraient partagé
// ses codes entre deux suivis, et la question « combien Awa a-t-elle apporté ? »
// n'aurait plus eu de réponse.
func (s *Service) CreateInfluencer(ctx context.Context, userID, handle, network, note string, audience int) (*Influencer, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, apperr.Validation("invalid user id")
	}
	if !ValidNetwork(network) {
		return nil, apperr.Validation("unknown network").
			WithMeta(map[string]any{"fields": []string{"network"}, "allowed": Networks})
	}
	h := NormaliseHandle(handle)
	if h == "" {
		return nil, apperr.Validation("a handle is required")
	}
	existing, err := s.repo.InfluencerByUser(ctx, oid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if existing != nil {
		return nil, errInfluencerExists.WithMeta(map[string]any{"handle": existing.Handle})
	}
	i := &Influencer{
		UserID: oid, Country: country.FromContext(ctx), Handle: h,
		Network: network, Audience: audience, Note: note, Active: true,
	}
	if err := s.repo.CreateInfluencer(ctx, i); err != nil {
		if err == ErrDuplicateHandle {
			return nil, apperr.Conflict("influencer_handle_taken", "this handle is already taken")
		}
		return nil, apperr.Internal(err)
	}
	if s.audit != nil {
		s.audit.Record(ctx, "influencer.create", "influencer", i.ID.Hex(), nil,
			map[string]any{"handle": i.Handle, "network": i.Network, "audience": i.Audience})
	}
	slog.InfoContext(ctx, "promocode: influencer created", "handle", i.Handle, "user_id", userID)
	return i, nil
}
