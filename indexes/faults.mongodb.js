// LES PANNES de la plateforme — voir `internal/faults`.
//
// ⚠️ Ce fichier DOCUMENTE le schéma ; ce sont les déclarations de
// `internal/indexes/indexes.go` qui sont réellement posées au démarrage, et
// un test refuse tout écart entre les deux.
//
// ⚠️ UNE LIGNE PAR EMPREINTE, et l'unicité le garantit. Sans elle, deux
// occurrences simultanées de la même panne — ce qui est exactement ce qui
// arrive quand une route casse — créeraient deux lignes, et le groupement,
// qui fait tout l'intérêt de ce registre, s'effondrerait au pire moment.
db.platform_faults.createIndex({ fingerprint: 1 }, { unique: true });

// La liste se lit PAR DERNIÈRE OCCURRENCE : une panne qui arrive trois mille
// fois par jour depuis six mois est connue ; celle qui vient d'apparaître est
// celle qu'on cherche.
db.platform_faults.createIndex({ last_seen: -1 });
db.platform_faults.createIndex({ service: 1, last_seen: -1 });
db.platform_faults.createIndex({ resolved_at: 1, last_seen: -1 });

// TRENTE JOURS. Garder pour toujours remplit la base de pannes corrigées il y
// a un an ; garder deux jours fait disparaître celle qui n'arrive qu'au
// moment de la paie.
db.platform_faults.createIndex({ last_seen: 1 }, { expireAfterSeconds: 2592000 });
