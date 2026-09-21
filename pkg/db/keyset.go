package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// LES LISTES « LES PLUS RÉCENTES D'ABORD ».
//
// Une liste de courses ou de commandes se lit du plus récent au plus ancien,
// et « récent » veut dire `created_at` — pas l'identifiant. Les deux se
// confondent presque toujours (l'identifiant porte la seconde d'insertion),
// mais un jeu de démonstration antidaté, une reprise de données ou deux
// écritures dans la même seconde les séparent, et l'écran range alors une
// commande d'hier au-dessus de celle de ce matin.
//
// Le curseur reste l'IDENTIFIANT de la dernière ligne lue : le format public
// (`?cursor=<id>`) ne change pas, le serveur relit la date de cette ligne
// pour reprendre strictement après elle.

// NewestFirst est le tri des listes de ce genre : la date de création, puis
// l'identifiant pour départager deux lignes nées au même instant.
var NewestFirst = bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}

// AfterNewest borne `filter` strictement APRÈS `cursor` dans l'ordre
// NewestFirst. Un curseur nul ne borne rien.
//
// La ligne du curseur peut avoir disparu (une commande purgée) : on retombe
// alors sur l'ordre des identifiants, qui suit l'insertion — la page suivante
// reste juste, elle ne saute pas.
//
// La borne s'ajoute en `$and` : elle ne remplace jamais un `_id` ou un `$or`
// que le filtre porterait déjà.
func AfterNewest(ctx context.Context, col *mongo.Collection, filter bson.M, cursor primitive.ObjectID) error {
	if cursor.IsZero() {
		return nil
	}
	var last struct {
		CreatedAt time.Time `bson:"created_at"`
	}
	err := col.FindOne(ctx, bson.M{"_id": cursor}, options.FindOne().SetProjection(bson.M{"created_at": 1})).Decode(&last)
	var clause bson.M
	switch {
	case errors.Is(err, mongo.ErrNoDocuments) || (err == nil && last.CreatedAt.IsZero()):
		clause = bson.M{"_id": bson.M{"$lt": cursor}}
	case err != nil:
		return fmt.Errorf("db: cursor %s: %w", cursor.Hex(), err)
	default:
		clause = bson.M{"$or": []bson.M{
			{"created_at": bson.M{"$lt": last.CreatedAt}},
			{"created_at": last.CreatedAt, "_id": bson.M{"$lt": cursor}},
		}}
	}
	and, _ := filter["$and"].([]bson.M)
	filter["$and"] = append(and, clause)
	return nil
}
