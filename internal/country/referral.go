package country

// LE PARRAINAGE — ce qu'il donne, pays par pays.
//
// « Invite un ami » est une dépense d'acquisition : à chaque filleul qui
// commande, la plateforme sort deux sommes de sa poche — une remise pour celui
// qui arrive, un crédit pour celui qui l'a amené. Combien, et jusqu'où, est une
// décision de direction, prise par marché et révisée souvent.
//
// ⚠️ PAR PAYS, ET NON GLOBAL. Le coût d'acquisition d'un client n'a pas le même
// ordre de grandeur à Lomé, à Dakar et à Conakry — ni le même panier moyen pour
// l'amortir. Un montant unique aurait été soit ridicule dans un pays, soit
// ruineux dans l'autre ; et il aurait fallu le changer pour tout le monde à la
// fois pour corriger un seul marché.
//
// ⚠️ ÉTEINT PAR DÉFAUT, DONC AUCUN PARRAINAGE TANT QUE PERSONNE N'A DÉCIDÉ.
// C'est la seule valeur par défaut défendable pour un réglage qui DISTRIBUE DE
// L'ARGENT : un parrainage actif d'office aurait commencé à payer dans cinq
// pays le jour du déploiement, sans budget, sans que personne ne l'ait voulu —
// et on l'aurait découvert sur le grand livre.
//
// ⚠️ ET SON EXTINCTION N'AFFICHE PAS DE BOUTON QUI NE DONNE RIEN : tant que la
// remise du filleul est à zéro, le socle ne tire même pas de code
// (`promocode.MyReferral`), et l'écran « inviter un ami » n'a rien à montrer.
// Un code partagé de bonne foi qui répondrait « invalide » aux proches de
// quelqu'un coûte plus cher que l'absence du bouton.

import (
	"context"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Défauts du parrainage, quand le pays n'a rien réglé.
const (
	// defaultReferralValidDays : un code de parrainage vit un an. Une durée,
	// même longue, plutôt que l'éternité : un code sans fin survit à la
	// campagne qui l'a créé, et continue de payer des montants décidés deux
	// directions plus tôt.
	defaultReferralValidDays = 365
	// maxReferralXOF borne ce que la console peut écrire d'un seul geste :
	// 50 000 F, soit une vingtaine de courses pour un seul parrainage.
	//
	// ⚠️ C'EST UN GARDE-FOU D'ORDRE DE GRANDEUR, PAS UN DÉTECTEUR DE FAUTE DE
	// FRAPPE, et il ne faut pas en attendre plus. Il arrête « 2000000 » — le
	// genre de nombre qui vide un budget en un après-midi et qu'aucun marché ne
	// justifie. Il ne peut PAS arrêter « 20000 » au lieu de « 2000 » : dix fois
	// trop, et pourtant une décision parfaitement possible là où le panier est
	// gros. Aucune borne ne distingue ces deux-là — seul le journal d'audit le
	// fait, après coup, en disant qui a écrit quoi et quand.
	maxReferralXOF = 50_000
)

// Referral est la politique de parrainage d'un pays, telle qu'elle est écrite
// en base.
//
// ⚠️ DEUX MONTANTS ET NON UN, parce que ce ne sont pas deux fois la même
// chose : la remise du FILLEUL porte sur ce qu'il paie (il ne connaît pas
// encore Dira, il faut qu'il essaie), et le crédit du PARRAIN atterrit sur son
// solde (il connaît Dira, il faut qu'il revienne). Les confondre aurait forcé à
// offrir la même somme aux deux, alors que leur coût et leur effet n'ont rien à
// voir — et que l'un se règle d'avance, l'autre après coup.
type Referral struct {
	// InviteeXOF : la remise du filleul, sur sa première opération. Zéro =
	// pas de parrainage dans ce pays.
	InviteeXOF int `bson:"invitee_xof,omitempty"`
	// SponsorXOF : le crédit du parrain, versé quand la remise du filleul est
	// VRAIMENT consommée — pas à la saisie du code.
	SponsorXOF int `bson:"sponsor_xof,omitempty"`
	// MaxSponsored : combien de filleuls un parrain peut récolter.
	//
	// ⚠️ ZÉRO = SANS LIMITE, et c'est rarement ce qu'on veut : un code de
	// parrainage sans plafond posté sur un groupe de mille personnes n'est
	// plus un parrainage, c'est une campagne que personne n'a budgétée.
	MaxSponsored int `bson:"max_sponsored,omitempty"`
	// ValidDays : la durée de vie du code d'un parrain. Vide = un an.
	ValidDays int `bson:"valid_days,omitempty"`
}

// ReferralResponse est la politique telle que la console la lit — et la seule
// forme où le champ calculé `active` existe.
type ReferralResponse struct {
	InviteeXOF   int `json:"invitee_xof"`
	SponsorXOF   int `json:"sponsor_xof"`
	MaxSponsored int `json:"max_sponsored"`
	ValidDays    int `json:"valid_days"`
	// Active RÉSUME ce que la console doit dire en une ligne : le parrainage
	// donne-t-il quelque chose, oui ou non ? Il est CALCULÉ de la remise du
	// filleul, et n'existe pas en base — un interrupteur enregistré à côté des
	// montants aurait permis les deux états absurdes : actif à zéro franc, ou
	// éteint avec des montants réglés que personne ne voit plus.
	Active bool `json:"active"`
}

// ReferralUpdateRequest règle le parrainage d'un pays. Tous les champs
// facultatifs — on change un montant sans retoucher les autres.
type ReferralUpdateRequest struct {
	InviteeXOF   *int `json:"invitee_xof" validate:"omitempty,min=0,max=50000"`
	SponsorXOF   *int `json:"sponsor_xof" validate:"omitempty,min=0,max=50000"`
	MaxSponsored *int `json:"max_sponsored" validate:"omitempty,min=0,max=100000"`
	ValidDays    *int `json:"valid_days" validate:"omitempty,min=1,max=3650"`
}

var errNoReferralUpdate = apperr.Validation(
	"nothing to update: send invitee_xof, sponsor_xof, max_sponsored and/or valid_days")

// tooLarge dit si un montant a l'air d'une faute de frappe plutôt que d'une
// décision. Voir `maxReferralXOF`.
func (r Referral) tooLarge() bool {
	return r.InviteeXOF > maxReferralXOF || r.SponsorXOF > maxReferralXOF
}

func referralResponse(r Referral) ReferralResponse {
	if r.ValidDays <= 0 {
		r.ValidDays = defaultReferralValidDays
	}
	return ReferralResponse{
		InviteeXOF: r.InviteeXOF, SponsorXOF: r.SponsorXOF,
		MaxSponsored: r.MaxSponsored, ValidDays: r.ValidDays,
		Active: r.InviteeXOF > 0,
	}
}

// Referral rend la politique de parrainage d'un pays, telle que la console la
// lit.
func (s *Service) Referral(ctx context.Context, code string) (*ReferralResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := referralResponse(inst.Referral)
	return &out, nil
}

// UpdateReferral règle le parrainage d'un pays.
func (s *Service) UpdateReferral(ctx context.Context, code string, req ReferralUpdateRequest) (*ReferralResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.InviteeXOF == nil && req.SponsorXOF == nil &&
		req.MaxSponsored == nil && req.ValidDays == nil {
		return nil, errNoReferralUpdate
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	ref := inst.Referral
	for _, f := range []struct {
		in  *int
		out *int
	}{
		{req.InviteeXOF, &ref.InviteeXOF}, {req.SponsorXOF, &ref.SponsorXOF},
		{req.MaxSponsored, &ref.MaxSponsored}, {req.ValidDays, &ref.ValidDays},
	} {
		if f.in != nil {
			*f.out = *f.in
		}
	}
	// ⚠️ La borne est vérifiée ICI AUSSI, et pas seulement par la balise de
	// validation : la balise garde la PORTE HTTP, cette ligne garde la
	// MÉTHODE. Un appel futur depuis une tâche ou un script de migration
	// n'aurait traversé aucun validateur, et un montant monstrueux se serait
	// écrit sans que rien ne le regarde.
	if ref.tooLarge() {
		return nil, errReferralTooLarge
	}
	if err := s.repo.SetReferral(ctx, info.Code, ref); err != nil {
		return nil, apperr.Internal(err)
	}
	if s.audit != nil {
		// ⚠️ TRACÉ, parce que ce réglage DISTRIBUE DE L'ARGENT. Le jour où le
		// grand livre montre dix fois plus de crédits de parrainage qu'attendu,
		// la seule question qui vaille est « qui a écrit ce montant, et
		// quand ? » — et un journal qui dirait seulement « le parrainage a été
		// modifié » n'y répondrait pas.
		s.audit.Record(ctx, "country.referral", "country", info.Code, nil,
			map[string]any{"referral": referralResponse(ref)})
	}
	out := referralResponse(ref)
	return &out, nil
}

// ReferralOf rend la politique de parrainage d'un pays — l'adaptateur que le
// registre des codes appelle quand il tire le code d'un parrain ou paie son
// crédit.
//
// ⚠️ AU MIEUX, ET AU PLUS FERMÉ : un pays inconnu ou une base muette rendent la
// politique VIDE, donc aucun parrainage — jamais une erreur. Le parrainage est
// un bonus ; faire échouer la fin d'une course parce que son réglage est
// illisible serait hors de proportion. Et rendre le DÉFAUT plutôt que le vide
// aurait distribué de l'argent précisément quand on ne sait plus quoi est réglé.
func (s *Service) ReferralOf(ctx context.Context, code string) Referral {
	info, ok := country.Lookup(code)
	if !ok {
		return Referral{}
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return Referral{}
	}
	r := inst.Referral
	if r.ValidDays <= 0 {
		r.ValidDays = defaultReferralValidDays
	}
	return r
}
