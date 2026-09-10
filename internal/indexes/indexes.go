// Package indexes déclare les index MongoDB du socle.
//
// La LISTE vit ici, le mécanisme de pose vit dans `pkg/db` : chaque service a
// ses collections, et aucun n'a à réimplémenter la pose, le repli ni le compte
// rendu.
//
// ⚠️ Ces déclarations DOUBLENT `indexes/*.mongodb.js`. Les fichiers .js
// documentent le schéma ; ce sont ces lignes-ci qui sont réellement appliquées,
// au démarrage. Un index qu'il faut penser à poser à la main finit toujours par
// manquer quelque part.
package indexes

import (
	"context"
	"log/slog"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/kgtech-org/dira-core-api/pkg/db"
)

var applicationIndexes = []db.Index{
	// --- user ---
	{Collection: "users", Keys: db.K("phone", 1), Unique: true},
	{Collection: "users", Keys: db.K("role", 1)},
	// L'email est facultatif : unique ET sparse, sinon deux comptes sans
	// email entreraient en collision sur la valeur nulle.
	// Unicité de l'e-mail SEULEMENT parmi ceux qui en ont un. On s'inscrit par
	// téléphone : la plupart des comptes n'ont pas d'e-mail, et `sparse` ne les
	// écarte que si le champ est absent — pas s'il vaut la chaîne vide. Deux
	// comptes sans e-mail suffisaient donc à empêcher l'index de se construire,
	// et le service démarrait sans lui.
	//
	// `$gt: ""` sélectionne les chaînes non vides : `$ne` n'est pas admis dans
	// une expression de filtre partiel.
	{
		Collection:    "users",
		Keys:          db.K("email", 1),
		Unique:        true,
		PartialFilter: bson.D{{Key: "email", Value: bson.D{{Key: "$gt", Value: ""}}}},
	},
	{Collection: "refresh_tokens", Keys: db.K("token_hash", 1), Unique: true},
	{Collection: "refresh_tokens", Keys: db.K("user_id", 1)},
	{Collection: "refresh_tokens", Keys: db.K("expires_at", 1), TTLSeconds: db.TTL(0)},
	// --- user (carnet d'adresses) ---
	// Le carnet d'un compte, l'adresse par défaut en tête — c'est celle que
	// l'application présélectionne.
	{Collection: "user_addresses", Keys: db.K("user_id", 1, "is_default", -1, "created_at", 1)},

	// --- token ---
	{Collection: "token_wallets", Keys: db.K("owner_id", 1), Unique: true},
	{Collection: "token_transactions", Keys: db.K("wallet_id", 1)},
	{Collection: "token_transactions", Keys: db.K("created_at", 1)},
	// La référence d'un mouvement : le COUPLE, pas l'identifiant seul. Deux
	// verticales peuvent porter le même identifiant — deux collections, deux
	// compteurs d'ObjectID, aucune garantie d'unicité entre elles.
	{Collection: "token_transactions", Keys: db.K("ref_kind", 1, "ref_id", 1), Sparse: true},
	// ⚠️ C'EST CET INDEX qui rend un mouvement d'argent rejouable sans danger.
	// La clé est le `_id` du marqueur — unique par construction — et la
	// réservation se fait DANS la transaction qui débite. Sans lui, une réponse
	// perdue et un réessai débitent deux fois le même client.
	//
	// TTL de 30 jours : ces marqueurs ne servent que le temps où un réessai est
	// plausible. Les garder indéfiniment ferait grossir une collection que
	// personne ne lit.
	{Collection: "wallet_operations", Keys: db.K("at", 1), TTLSeconds: db.TTL(30 * 24 * 3600)},

	// --- paiements ---
	{Collection: "payments", Keys: db.K("provider_ref", 1), Unique: true},
	{Collection: "payments", Keys: db.K("user_id", 1)},
	{Collection: "payments", Keys: db.K("status", 1)},

	// --- notifications ---
	{Collection: "push_devices", Keys: db.K("token", 1), Unique: true},
	{Collection: "push_devices", Keys: db.K("user_id", 1, "disabled_at", 1)},
	{Collection: "message_templates", Keys: db.K("key", 1), Unique: true},
	{Collection: "notifications", Keys: db.K("user_id", 1, "_id", -1)},
	{Collection: "notifications", Keys: db.K("user_id", 1, "read_at", 1)},

	// --- notes ---
	// UNE note par (commande, cible). L'unicité est portée par l'INDEX et non
	// par une lecture préalable : deux envois simultanés passeraient tous les
	// deux un contrôle applicatif et compteraient double.
	{Collection: "ratings", Keys: db.K("order_id", 1, "target_type", 1, "target_id", 1), Unique: true},
	// La liste des avis d'une cible, du plus récent au plus ancien.
	{Collection: "ratings", Keys: db.K("target_type", 1, "target_id", 1, "_id", -1)},

	// --- flottes privées ---
	//
	// ⚠️ Le NOM est UNIQUE, et c'est ce qui empêche deux exploitants traitant
	// le même contrat d'enregistrer deux fois la même société. Un contrôle
	// applicatif « ce nom existe-t-il ? » laisse passer les deux écritures
	// concurrentes, et la base porte alors deux flottes que rien ne distingue.
	{Collection: "fleets", Keys: db.K("name", 1), Unique: true},
	// La liste de l'administration, filtrée par état.
	{Collection: "fleets", Keys: db.K("status", 1, "_id", -1)},
	// « Quelle flotte gère ce compte ? » — la question que pose la connexion
	// d'un gérant. PARTIEL : une flotte enregistrée sur contrat papier n'a pas
	// encore de gérant, et indexer ces absences coûterait sans rien répondre.
	{Collection: "fleets", Keys: db.K("owner_user_id", 1),
		PartialFilter: bson.D{{Key: "owner_user_id", Value: bson.D{{Key: "$exists", Value: true}}}}},

	// --- staff ---
	//
	// ⚠️ UNE fiche par compte. Deux fiches contradictoires décideraient des
	// droits d'une personne selon celle qu'on lit en premier — et un contrôle
	// applicatif « a-t-il déjà une fiche ? » laisse passer deux créations
	// concurrentes.
	{Collection: "staff_members", Keys: db.K("user_id", 1), Unique: true},
	// La liste de l'administration, filtrée par fonction ou par état.
	{Collection: "staff_members", Keys: db.K("function", 1, "_id", -1)},
	{Collection: "staff_members", Keys: db.K("status", 1, "_id", -1)},
}

// Ensure pose les index du socle. Idempotent : Mongo ignore un index déjà
// présent, et un index qui échoue est journalisé sans arrêter le démarrage.
func Ensure(ctx context.Context, database *mongo.Database, logger *slog.Logger) int {
	return db.EnsureIndexes(ctx, database, logger, applicationIndexes)
}
