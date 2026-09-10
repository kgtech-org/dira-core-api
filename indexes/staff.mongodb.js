// Index du STAFF de Dira (collection staff_members).
// Run with: mongosh <uri> indexes/staff.mongodb.js
//
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés sont
// déclarés dans internal/indexes/indexes.go et créés au démarrage de l'API ;
// un test refuse tout écart entre les deux.
//
// Une fiche de staff n'est PAS un compte : elle attache une fonction et un
// PÉRIMÈTRE à un compte `admin` existant. Le périmètre est inscrit dans le
// jeton à la connexion et vérifié par chaque verticale.

// UNE fiche par compte.
//
// ⚠️ Deux fiches contradictoires décideraient des droits d'une personne selon
// celle qu'on lit en premier. Un contrôle applicatif « a-t-il déjà une
// fiche ? » laisse passer deux créations concurrentes — deux exploitants
// enregistrant la même embauche au même moment.
db.staff_members.createIndex({ user_id: 1 }, { unique: true });

// La liste de l'administration : par fonction, par état, les plus récents
// d'abord.
db.staff_members.createIndex({ function: 1, _id: -1 });
db.staff_members.createIndex({ status: 1, _id: -1 });
