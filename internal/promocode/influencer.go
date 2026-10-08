package promocode

// LES COMPTES D'INFLUENCEURS — qui diffuse un code, et ce que ça a donné.
//
// ⚠️ UN PROFIL SUR UN COMPTE, PAS UN RÔLE DE PLUS. Un influenceur est quelqu'un
// qui a un compte Dira — souvent client lui-même — et à qui on attache une
// fiche : un pseudonyme, un réseau, des codes. C'est exactement la mécanique
// d'un chauffeur (un compte + un profil de conduite) et d'un membre du staff
// (un compte + des portées).
//
// Un RÔLE aurait coûté une entrée dans le jeton, une porte de connexion
// (`app`), un gabarit de notification et une colonne dans chaque écran de
// comptes — pour quelqu'un qui n'ouvre aucune application.
//
// ⚠️ ET IL N'Y A PAS DE PAIEMENT ICI, exprès. Rémunérer un influenceur demande
// un contrat, un barème, un échéancier et un moyen de verser : c'est le
// matériel et les abonnements qui ont ce genre de machinerie, et elle ne
// s'improvise pas dans un coin. Ce module répond à « combien ce code a-t-il
// servi, et combien ça nous a coûté » ; ce qu'on en fait est une décision
// commerciale, prise avec ces chiffres sous les yeux.

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Influencer est la fiche de quelqu'un qui diffuse des codes.
type Influencer struct {
	ID primitive.ObjectID `bson:"_id,omitempty"`
	// UserID est le COMPTE au socle. Un influenceur n'est pas un compte à
	// part : c'est une fiche attachée à quelqu'un qui existe déjà.
	//
	// ⚠️ ET C'EST CE QUI FAIT QUE SA SUPPRESSION DE COMPTE L'EMPORTE. Une
	// fiche attachée à un compte effacé n'a plus de titulaire ; le suivi, lui,
	// reste — ce sont des usages, pas une identité.
	UserID  primitive.ObjectID `bson:"user_id"`
	Country string             `bson:"country,omitempty"`
	// Handle est le pseudonyme sous lequel on le connaît — « @awa.lome ».
	// Unique, parce que c'est ce qu'on tape pour le retrouver.
	Handle string `bson:"handle"`
	// Network et Audience : où il publie, et combien de personnes le suivent.
	//
	// ⚠️ DÉCLARATIFS, et il faut le savoir en les lisant : personne ici ne
	// vérifie un nombre d'abonnés. Ils servent à trier une liste et à préparer
	// une conversation, jamais à décider d'un budget — c'est le SUIVI des
	// usages qui dit ce qu'un influenceur vaut, et lui seul.
	Network  string `bson:"network,omitempty"`
	Audience int    `bson:"audience,omitempty"`
	Active   bool   `bson:"active"`
	Note     string `bson:"note,omitempty"`

	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

// Les réseaux qu'on sait nommer. La liste vit ICI : c'est le serveur qui
// refuse une valeur inconnue, et la console qui la lit pour la proposer — deux
// listes divergeraient au premier réseau ajouté.
var Networks = []string{"tiktok", "instagram", "facebook", "youtube", "x", "whatsapp", "radio", "terrain", "autre"}

// ValidNetwork dit si ce réseau est connu. Vide est admis : on peut inscrire
// quelqu'un avant de savoir où il publie.
func ValidNetwork(n string) bool {
	if n == "" {
		return true
	}
	for _, v := range Networks {
		if v == n {
			return true
		}
	}
	return false
}

// Stats est ce qu'un code a donné — la réponse à « est-ce que ça marche ? ».
//
// ⚠️ TROIS CHIFFRES ET PAS UN, parce qu'un seul mentirait :
//
//   - USES dit combien de fois le code a servi. C'est ce qu'un influenceur
//     regarde, et c'est le plus flatteur.
//   - PEOPLE dit combien de personnes DIFFÉRENTES l'ont utilisé. L'écart avec
//     `uses` est exactement ce qu'on veut voir : cent usages par trois
//     personnes n'est pas une campagne, c'est une fuite.
//   - DISCOUNT dit ce que ça nous a coûté. Sans lui, « mille usages » se lit
//     comme un succès alors que c'est peut-être une facture.
type Stats struct {
	Uses        int `json:"uses"`
	People      int `json:"people"`
	DiscountXOF int `json:"discount_xof"`
	// Reserved et Released : les usages en cours, et ceux qui ont été rendus
	// (course annulée, commande refusée).
	//
	// ⚠️ `RELEASED` EST AFFICHÉ, et ce n'est pas du détail : un code dont la
	// moitié des usages sont rendus signale soit une fraude — commander puis
	// annuler pour épuiser l'enveloppe —, soit une offre qui attire des gens
	// qui n'achètent pas. Les deux méritent qu'on les voie.
	Reserved int `json:"reserved"`
	Released int `json:"released"`
}

// NormaliseHandle met un pseudonyme sous sa forme canonique.
//
// ⚠️ SANS LE `@`, et en minuscules. Les gens le recopient avec l'arobase
// depuis un profil ; le garder ferait deux fiches pour « awa.lome » et
// « @awa.lome », et la recherche ne trouverait qu'une des deux.
func NormaliseHandle(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	return strings.TrimPrefix(s, "@")
}
