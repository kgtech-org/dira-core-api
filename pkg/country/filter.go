package country

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
)

// Field est le nom du champ `country` sur tout document borné par pays :
// compte, commande, course, enseigne, chauffeur, campagne. Le même nom
// partout, pour que la migration et les index s'écrivent une fois.
const Field = "country"

// Restrict ajoute le pays effectif de la requête à un filtre Mongo.
//
// ⚠️ C'est LA borne « haut niveau » de la plateforme : toute liste que
// l'exploitation ou le public consulte doit passer par ici, sans quoi la
// console de Lomé affiche les courses de Cotonou. La borne est posée dans le
// DÉPÔT, pas dans le handler : un handler oublié laisserait passer, un dépôt
// oublié n'existe pas — c'est lui qu'on relit.
//
// Sans pays dans le contexte (middleware absent, ou tâche de fond sans
// requête), le filtre est rendu tel quel : une tâche de fond qui consolide
// tous les pays est légitime, et un filtre sur `country: ""` n'aurait rien
// rendu du tout — ce qui se serait lu comme une base vide.
func Restrict(ctx context.Context, filter bson.M) bson.M {
	if filter == nil {
		filter = bson.M{}
	}
	if code := FromContext(ctx); code != "" {
		filter[Field] = code
	}
	return filter
}

// RestrictD est `Restrict` pour un filtre ordonné.
func RestrictD(ctx context.Context, filter bson.D) bson.D {
	if code := FromContext(ctx); code != "" {
		filter = append(filter, bson.E{Key: Field, Value: code})
	}
	return filter
}
