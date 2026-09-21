// Le MATÉRIEL loué ou vendu aux agents — voir `internal/equipment`.
//
// ⚠️ Ce fichier DOCUMENTE le schéma ; ce sont les déclarations de
// `internal/indexes/indexes.go` qui sont réellement posées au démarrage, et
// un test refuse tout écart entre les deux.
db.equipment_items.createIndex({ country: 1, active: 1, _id: -1 });
db.equipment_contracts.createIndex({ user_id: 1, status: 1, _id: 1 });
db.equipment_contracts.createIndex({ country: 1, status: 1, _id: -1 });
db.equipment_contracts.createIndex({ status: 1, next_period_at: 1 });
db.equipment_contracts.createIndex({ item_id: 1, _id: -1 });
