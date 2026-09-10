package token

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Ce fichier empêche qu'un même mouvement d'argent s'applique DEUX FOIS.
//
// Le besoin est né de l'extraction : `PayOrder` était un appel de fonction,
// c'est devenu un appel HTTP. Une réponse perdue — le débit appliqué, la
// réponse jamais arrivée — laisse l'appelant sans nouvelle. S'il réessaie, il
// débite deux fois ; s'il abandonne, le client est débité sans commande. Aucun
// des deux n'est acceptable, et le réseau ne dit pas lequel s'est produit.
//
// ⚠️ La clé est DÉRIVÉE de l'opération, pas fournie par l'appelant. Une
// commande se paie une fois : son identifiant EST la clé. Demander une clé à
// l'appelant aurait mis la garantie à la charge de celui qui a le plus de
// raisons de l'oublier — et un oubli ne se voit pas, jusqu'au jour où un
// client est débité deux fois.

const operationsCollection = "wallet_operations"

// errOperationApplied dit qu'une opération a DÉJÀ été appliquée.
//
// Elle ne remonte pas jusqu'à l'appelant : une opération déjà appliquée est un
// SUCCÈS de son point de vue — il voulait que l'argent bouge, l'argent a
// bougé. Lui rendre une erreur le pousserait à réessayer, ou à annuler une
// commande parfaitement payée.
var errOperationApplied = errors.New("token: operation already applied")

// operationKeys : la clé naturelle de chaque mouvement rattaché à un objet.
// Le GENRE fait partie de la clé : une commande et une course peuvent porter le
// même identifiant sans jamais se confondre — deux collections, deux compteurs
// d'ObjectID, aucune garantie d'unicité entre elles.
func payKey(refKind, refID string) string    { return "payment:" + refKind + ":" + refID }
func refundKey(refKind, refID string) string { return "refund:" + refKind + ":" + refID }

// earningsKey distingue le bénéficiaire : une même commande verse à la
// boutique ET au livreur, et ces deux versements ne sont pas le même
// mouvement.
func earningsKey(ownerID, refKind, refID, reason string) string {
	return "earnings:" + reason + ":" + ownerID + ":" + refKind + ":" + refID
}

// consumeKey identifie une dépense de jetons rattachée à une commande — un
// livreur qui accepte, un marchand qui propulse.
func consumeKey(ownerID, refKind, refID, reason string) string {
	return "consume:" + reason + ":" + ownerID + ":" + refKind + ":" + refID
}

// ClaimOperation réserve une opération, ou dit qu'elle a déjà eu lieu.
//
// ⚠️ Doit être appelée DANS la transaction qui applique le mouvement. Hors
// transaction, une panne entre la réservation et le débit bloquerait l'argent
// pour toujours : l'opération serait marquée faite sans l'être.
//
// C'est l'index unique sur `_id` qui garantit l'unicité, pas une lecture
// préalable : deux appels simultanés liraient tous deux « absent ».
func (r *Repository) ClaimOperation(ctx context.Context, key string) error {
	_, err := r.operations.InsertOne(ctx, bson.M{
		"_id": key,
		// `at` porte l'index TTL : ces marqueurs ne servent que le temps où un
		// réessai est plausible. Les garder indéfiniment ferait grossir une
		// collection dont personne ne lit jamais le contenu.
		"at": time.Now().UTC(),
	})
	if mongo.IsDuplicateKeyError(err) {
		return errOperationApplied
	}
	if err != nil {
		return apperr.Internal(err)
	}
	return nil
}
