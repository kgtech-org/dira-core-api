// Le journal d'audit de TOUTE la plateforme — voir `pkg/audit`.
//
// ⚠️ Ce fichier DOCUMENTE le schéma ; ce sont les déclarations de
// `internal/indexes/indexes.go` qui sont réellement posées au démarrage, et
// un test refuse tout écart entre les deux.
// Le PAYS est en tête : toute lecture du journal est bornée par le pays de
// la requête, donc aucune n'attaque ces index sans lui.
db.audit_logs.createIndex({ country: 1, _id: -1 });
db.audit_logs.createIndex({ country: 1, service: 1, _id: -1 });
db.audit_logs.createIndex({ country: 1, actor_id: 1, _id: -1 });
db.audit_logs.createIndex({ country: 1, action: 1, _id: -1 });
db.audit_logs.createIndex({ country: 1, "resource.id": 1, _id: -1 });
db.audit_logs.createIndex({ created_at: -1 });
