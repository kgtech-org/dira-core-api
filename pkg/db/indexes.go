package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Index déclaré une fois, appliqué à chaque démarrage.
//
// Ces déclarations DOUBLENT `indexes/*.mongodb.js`, et un test refuse tout
// écart entre les deux (voir indexes_test.go). Les fichiers .js restent la
// référence lisible du schéma ; ceci est ce qui s'exécute réellement.
//
// La raison d'être de ce fichier : les .js n'étaient appliqués par rien —
// ni cible make, ni code — et l'absence de l'index 2dsphere sur `stores` a
// produit un 500 en production sur `GET /stores` (`unable to find index for
// $geoNear query`) alors que tout fonctionnait en local, où l'index avait été
// créé à la main. Un index qu'il faut penser à poser finit toujours par
// manquer quelque part.
// ensureIndexTimeout borne la pose des index au démarrage. Les collections
// visées sont petites et createIndex est presque instantané ; au-delà, mieux
// vaut servir avec un index manquant que ne pas démarrer du tout.
const ensureIndexTimeout = 60 * time.Second

type Index struct {
	Collection string
	Keys       bson.D
	Unique     bool
	Sparse     bool
	// PartialFilter restreint l'index aux documents qui satisfont l'expression.
	//
	// Nécessaire là où `sparse` ne suffit pas : sparse n'écarte que les champs
	// ABSENTS, jamais une chaîne VIDE. Sur une plateforme où l'on s'inscrit par
	// téléphone, la plupart des comptes n'ont pas d'e-mail — dès que deux
	// d'entre eux portent `email: ""`, un index unique refuse de se construire.
	PartialFilter bson.D
	// TTLSeconds, quand il est non nil, purge les documents ce nombre de
	// secondes après la date portée par le champ indexé.
	TTLSeconds *int32
}

// K builds an ordered key document from alternating name/value pairs:
// K("store_id", 1, "dish_id", 1). Panique à l'initialisation sur un nombre
// impair d'arguments — c'est une faute de programmation, pas une erreur
// d'exécution.
func K(pairs ...any) bson.D {
	if len(pairs)%2 != 0 {
		panic("db: index keys must be name/value pairs")
	}
	d := make(bson.D, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		name, ok := pairs[i].(string)
		if !ok {
			panic(fmt.Sprintf("db: index key name must be a string, got %T", pairs[i]))
		}
		d = append(d, bson.E{Key: name, Value: pairs[i+1]})
	}
	return d
}

// TTL déclare une purge automatique N secondes après la date indexée.
func TTL(seconds int32) *int32 { return &seconds }

// EnsureIndexes pose les index DÉCLARÉS par le service appelant.
//
// La liste vient du service, le mécanisme vit ici : chaque verticale a ses
// collections, et aucune n'a à réimplémenter la pose, le repli ni le compte
// rendu. Un index qui échoue est journalisé À VOIX HAUTE et n'arrête pas le
// démarrage — une requête lente vaut mieux qu'un service qui refuse de partir.
func EnsureIndexes(ctx context.Context, database *mongo.Database, logger *slog.Logger, specs []Index) int {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithTimeout(ctx, ensureIndexTimeout)
	defer cancel()

	failed := 0
	for _, spec := range specs {
		opts := options.Index()
		if spec.Unique {
			opts.SetUnique(true)
		}
		if spec.Sparse {
			opts.SetSparse(true)
		}
		if spec.PartialFilter != nil {
			opts.SetPartialFilterExpression(spec.PartialFilter)
		}
		if spec.TTLSeconds != nil {
			opts.SetExpireAfterSeconds(*spec.TTLSeconds)
		}
		model := mongo.IndexModel{Keys: spec.Keys, Options: opts}
		if _, err := database.Collection(spec.Collection).Indexes().CreateOne(ctx, model); err != nil {
			failed++
			logger.ErrorContext(ctx, "db: index creation failed",
				"collection", spec.Collection, "keys", spec.Keys.Map(), "error", err)
		}
	}
	if failed > 0 {
		logger.ErrorContext(ctx, "db: some indexes are missing, queries may fail or scan whole collections",
			"failed", failed, "total", len(specs))
	} else {
		logger.InfoContext(ctx, "db: indexes ensured", "count", len(specs))
	}
	return failed
}
