package fleet

// LA FLOTTE VUE PAR SON PARTENAIRE — et la seule règle qui tient tout
// l'isolement de la console partenaire.
//
// ⚠️⚠️ LA FLOTTE VIENT DU JETON, JAMAIS D'UN PARAMÈTRE. C'est la phrase à
// retenir de ce fichier, et c'est pourquoi il n'y a PAS de fonction qui prenne
// un identifiant de flotte en entrée : un `GET /partner/fleets/{id}` aurait
// suffi à un partenaire curieux pour lire le parc d'un concurrent en changeant
// un chiffre dans l'URL. Toute la surface partenaire part donc de
// `FleetOfOwner(ctx, userID)` — l'identifiant du compte connecté, et rien
// d'autre.
//
// ⚠️ ET UN PARTENAIRE SANS FLOTTE EST REFUSÉ EXPLICITEMENT, pas servi à vide.
// Un compte `partner` créé avant que son contrat soit enregistré verrait sinon
// une console vierge et croirait à une panne. Le refus nomme le problème, et
// c'est l'exploitation qui le règle en rattachant la flotte.

import (
	"context"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

var (
	// errNoFleet : le compte est bien un partenaire, mais aucune flotte ne lui
	// est rattachée.
	//
	// ⚠️ DISTINCT D'UN REFUS D'ACCÈS, et c'est délibéré : « vous n'avez pas de
	// flotte » se règle par un appel à l'exploitation, « interdit » fait croire
	// à une erreur de sa part. Le message dit quoi faire.
	errNoFleet = apperr.Forbidden("partner_no_fleet",
		"no fleet is attached to this account yet: ask Dira to link your contract")
	// errFleetSuspended : le contrat de la flotte n'est plus actif.
	//
	// ⚠️ LA CONSOLE SE FERME, LES CHAUFFEURS CONTINUENT. Suspendre une flotte ne
	// suspend pas ses conducteurs — ce sont deux décisions distinctes, et
	// cascader aurait mis dix personnes à l'arrêt pour un papier non signé.
	// Le partenaire, lui, ne gère plus rien en attendant.
	errFleetSuspended = apperr.Forbidden("partner_fleet_suspended",
		"this fleet's contract is suspended: contact Dira")
)

// FleetOfOwner rend la flotte d'un compte partenaire.
//
// ⚠️ LA SEULE PORTE DE LA SURFACE PARTENAIRE. Tout ce qu'un partenaire lit ou
// écrit doit passer par l'identifiant qu'elle rend ; aucune route ne doit
// accepter une flotte en paramètre, même « pour la commodité ».
func (s *Service) FleetOfOwner(ctx context.Context, userID string) (*Response, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errNoFleet
	}
	f, err := s.repo.ByMember(ctx, uid)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if f == nil {
		return nil, errNoFleet
	}
	if f.Status == StatusSuspended {
		return nil, errFleetSuspended
	}
	out := toResponse(f)
	// ⚠️ LE RÔLE VOYAGE AVEC LA FLOTTE, et c'est la moitié de la réponse : la
	// console doit savoir si elle parle au propriétaire, à un répartiteur ou à
	// un comptable AVANT de dessiner un bouton. Un bouton affiché puis refusé
	// par le serveur a déjà promis quelque chose.
	out.Role = roleIn(f, uid)
	return &out, nil
}

// FleetIDOfOwner rend l'identifiant seul — ce que les verticales demandent
// pour borner leurs listes.
//
// ⚠️ ELLE REND LA MÊME VÉRIFICATION QUE `FleetOfOwner` : une verticale qui
// n'aurait récupéré qu'un identifiant sans contrôler le statut aurait laissé un
// partenaire suspendu continuer à gérer son parc par une autre porte.
func (s *Service) FleetIDOfOwner(ctx context.Context, userID string) (string, error) {
	f, err := s.FleetOfOwner(ctx, userID)
	if err != nil {
		return "", err
	}
	return f.ID, nil
}
