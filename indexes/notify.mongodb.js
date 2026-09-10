// Indexes for the notify module (push_devices, message_templates).
// Run with: mongosh <uri> indexes/notify.mongodb.js
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés sont
// déclarés dans internal/platform/db/indexes.go et créés au démarrage de
// l'API ; un test refuse tout écart entre les deux.

// UNE ligne par jeton FCM. C'est l'unicité qui permet de RÉATTRIBUER un
// téléphone : changer de compte sur le même appareil garde le jeton, et sans
// cet index l'ancien utilisateur continuerait de recevoir les notifications
// du nouveau.
db.push_devices.createIndex({ token: 1 }, { unique: true });

// Les appareils JOIGNABLES d'un utilisateur — lus à chaque notification.
db.push_devices.createIndex({ user_id: 1, disabled_at: 1 });

// Un gabarit par clé.
db.message_templates.createIndex({ key: 1 }, { unique: true });

// Le centre de notifications : la liste d'un compte, de la plus récente. Une
// notification poussée ne laisse aucune trace — un téléphone éteint, une
// bannière balayée, et le message n'existe plus nulle part.
db.notifications.createIndex({ user_id: 1, _id: -1 });

// Le badge de non-lus, lu à chaque ouverture de l'écran Compte.
db.notifications.createIndex({ user_id: 1, read_at: 1 });
