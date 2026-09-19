// Le journal d'audit de TOUTE la plateforme — voir `pkg/audit`.
//
// ⚠️ Ce fichier DOCUMENTE le schéma ; ce sont les déclarations de
// `internal/indexes/indexes.go` qui sont réellement posées au démarrage, et
// un test refuse tout écart entre les deux.
db.audit_logs.createIndex({ service: 1, _id: -1 });
db.audit_logs.createIndex({ actor_id: 1, _id: -1 });
db.audit_logs.createIndex({ action: 1, _id: -1 });
db.audit_logs.createIndex({ "resource.id": 1, _id: -1 });
db.audit_logs.createIndex({ created_at: -1 });
