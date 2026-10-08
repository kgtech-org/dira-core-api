// Package promocode porte les CODES PROMO de toute la plateforme : un mot que
// quelqu'un saisit, et une remise qui s'applique — sur une course comme sur
// une commande.
//
// ⚠️ AU SOCLE, ET PAS DANS CHAQUE VERTICALE, et c'est la contrainte qui décide
// de tout le reste. Les promotions AUTOMATIQUES vivent dans les verticales, et
// c'est juste : une remise sur les plats d'une enseigne et une remise sur les
// courses éco n'ont rien à voir. Un CODE, lui, est un objet différent :
//
//   - IL DOIT ÊTRE UNIQUE SUR TOUTE LA PLATEFORME. Deux verticales qui
//     posséderaient chacune « DIRA10 » donneraient deux remises différentes au
//     même mot, et le client aurait raison de crier.
//   - SON ENVELOPPE EST UNE SEULE ENVELOPPE. Le code d'un influenceur vaut sur
//     une course ET sur une commande : un budget par verticale laisserait
//     dépenser deux fois ce qu'on avait prévu, et personne ne verrait le total
//     avant le relevé bancaire.
//   - SON SUIVI EST UN SEUL SUIVI. « Combien ce code a-t-il servi ? » n'a pas
//     de réponse si deux services comptent chacun la moitié.
//
// ⚠️ MAIS LE MOTEUR N'EST PAS RÉÉCRIT. L'enveloppe, les compteurs en deux
// temps (réservé puis dépensé), le registre idempotent par référence : tout
// vient de `pkg/promo`, celui-là même que les deux verticales utilisent pour
// leurs promotions automatiques. Un second moteur aurait été un second endroit
// où de l'argent se compte — et le jour où les deux divergent, personne ne
// sait lequel fait foi.
package promocode

import (
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/promo"
)

// Collections MongoDB du module.
const (
	Collection     = "promo_codes"
	CollectionUses = "promo_code_uses"
	// CollectionInfluencers : les comptes d'influenceurs, qui possèdent des
	// codes et dont on suit l'usage.
	CollectionInfluencers = "influencers"
)

// D'où vient un code — et c'est ce qui décide de qui le voit et qui en répond.
const (
	// KindCampaign : une opération de la plateforme. « RENTREE25 » sur une
	// affiche, dans un SMS, sur une radio. Personne ne le « possède ».
	KindCampaign = "campaign"
	// KindInfluencer : le code d'un INFLUENCEUR, qu'il diffuse et dont on
	// suit l'usage. C'est le suivi qui en fait un objet à part : sans lui,
	// l'influenceur n'a rien à montrer et nous rien à vérifier.
	KindInfluencer = "influencer"
	// KindReferral : le code de PARRAINAGE d'un client, qui invite ses
	// proches.
	//
	// ⚠️ IL RÉCOMPENSE LES DEUX CÔTÉS, et c'est ce qui le distingue d'un code
	// de campagne : le filleul obtient la remise, le parrain obtient un
	// crédit quand elle est VRAIMENT consommée. Récompenser à la saisie
	// aurait payé des parrainages que personne n'a honorés — il suffit de
	// commander puis d'annuler.
	KindReferral = "referral"
)

// Natures de la remise — le même vocabulaire que les promotions des
// verticales, exprès : une remise se lit pareil d'où qu'elle vienne.
const (
	KindPercent = "percent" // value = 1..100 (%)
	KindAmount  = "amount"  // value = XOF
)

// Les métiers où un code peut valoir.
const (
	VerticalVTC  = "vtc"
	VerticalFood = "food"
)

// Code est un mot qui donne droit à une remise.
type Code struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`
	// Code est le mot SAISI, en majuscules et sans espace.
	//
	// ⚠️ UNIQUE SUR TOUTE LA PLATEFORME (index unique), et normalisé à
	// l'écriture comme à la lecture : personne ne tape « dira10 » et
	// « DIRA 10 » en pensant à deux choses différentes, et un code refusé
	// pour une casse est un code qu'on croit cassé.
	Code string `bson:"code"`
	Kind string `bson:"kind"`
	// Country : un code vaut dans UN pays. Une opération lancée à Lomé n'a
	// aucune raison de remiser les courses de Dakar — et la monnaie n'est
	// même pas la même qu'en Guinée.
	Country string `bson:"country,omitempty"`
	// OwnerID : l'INFLUENCEUR ou le PARRAIN. Vide pour une campagne.
	OwnerID *primitive.ObjectID `bson:"owner_id,omitempty"`
	// Verticals : les métiers où le code vaut. VIDE = LES DEUX.
	//
	// ⚠️ Vide vaut « partout », et non « nulle part ». Un code créé sans
	// préciser doit marcher : l'inverse aurait fait des codes muets que
	// personne ne comprend, et dont on cherche le réglage manquant.
	Verticals []string `bson:"verticals,omitempty"`

	// La REMISE.
	DiscountKind string `bson:"discount_kind"`
	Value        int    `bson:"value"`
	// MaxDiscountXOF borne une remise en POURCENTAGE.
	//
	// ⚠️ MÊME RAISON QUE POUR LES COURSES : 20 % d'une course de quarante
	// kilomètres font trois mille francs offerts sur UNE course. Sans
	// plafond, une opération pensée pour les petits trajets se paie sur les
	// longs.
	MaxDiscountXOF int `bson:"max_discount_xof,omitempty"`
	// MinAmountXOF : en dessous de ce montant, le code ne s'applique pas.
	MinAmountXOF int `bson:"min_amount_xof,omitempty"`

	// L'ENVELOPPE et ce qui en a été consommé — le vocabulaire du socle, le
	// même que les promotions automatiques des verticales.
	promo.Limits   `bson:",inline"`
	promo.Counters `bson:",inline"`

	StartsAt time.Time `bson:"starts_at"`
	EndsAt   time.Time `bson:"ends_at"`
	Active   bool      `bson:"active"`

	// Label est ce que l'écran affiche à côté de la remise — « Rentrée 2025 »,
	// « Code de Awa ». Jamais la clé : un libellé change, un code non.
	Label     string    `bson:"label,omitempty"`
	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// Live dit si le code s'applique à l'instant t.
func (c *Code) Live(t time.Time) bool {
	return c.Active && !t.Before(c.StartsAt) && t.Before(c.EndsAt)
}

// Serves dit si le code vaut dans ce métier.
func (c *Code) Serves(vertical string) bool {
	if len(c.Verticals) == 0 {
		return true
	}
	for _, v := range c.Verticals {
		if v == vertical {
			return true
		}
	}
	return false
}

// DiscountOn rend la remise que ce code applique à un montant.
//
// ⚠️ ARRONDIE AU MULTIPLE INFÉRIEUR DE 50, et vers le BAS exprès : arrondir
// une remise vers le haut ferait sortir de l'enveloppe un franc que personne
// n'a budgété, et sur cent mille usages cela se voit. En dessous de 50 F, la
// remise ne vaut rien et vaut mieux ne pas être accordée du tout — un
// « −25 F » affiché fait plus de mal que pas de remise.
func (c *Code) DiscountOn(amountXOF int) int {
	if amountXOF <= 0 || amountXOF < c.MinAmountXOF {
		return 0
	}
	d := c.Value
	if c.DiscountKind == KindPercent {
		d = amountXOF * c.Value / 100
		if c.MaxDiscountXOF > 0 && d > c.MaxDiscountXOF {
			d = c.MaxDiscountXOF
		}
	}
	// ⚠️ JAMAIS PLUS QUE LE MONTANT. Une remise de 2 000 F sur une course de
	// 600 F rendrait un prix négatif, et quelque part quelqu'un paierait le
	// client pour rouler.
	if d > amountXOF {
		d = amountXOF
	}
	d = d / roundingXOF * roundingXOF
	return d
}

// roundingXOF : les remises s'arrondissent au multiple de 50, comme les prix.
// La plus petite pièce couramment rendue vaut 25 F.
const roundingXOF = 50

// codePattern : ce qu'un code peut contenir.
//
// ⚠️ MAJUSCULES ET CHIFFRES SEULEMENT, 4 à 24 caractères. Pas de tiret, pas
// d'espace, pas d'accent : un code se dicte au téléphone, se lit sur une
// affiche et se tape sur un clavier mobile. Chaque caractère ambigu est un
// code que quelqu'un n'arrivera pas à saisir, et un support qui le réexplique.
var codePattern = regexp.MustCompile(`^[A-Z0-9]{4,24}$`)

// Normalise met un code saisi sous sa forme canonique.
//
// ⚠️ ELLE RETIRE LES ESPACES ET LES TIRETS plutôt que de les refuser : les
// gens recopient « DIRA-10 » depuis une affiche, et refuser ce qu'on leur a
// montré est notre faute, pas la leur.
func Normalise(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

// ValidCode dit si un code est écrivable.
func ValidCode(code string) bool { return codePattern.MatchString(code) }
