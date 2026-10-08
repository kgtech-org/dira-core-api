// MongoDB indexes for the promo-code module (internal/promocode).
// ⚠️ Ce fichier ne s'exécute PAS tout seul. Les index réellement posés sont
// déclarés dans internal/indexes/indexes.go et créés au démarrage de l'API ;
// un test refuse tout écart entre les deux.

// ⚠️ LE CODE EST UNIQUE SUR TOUTE LA PLATEFORME. Deux verticales — ou deux
// pays — qui posséderaient « DIRA10 » donneraient deux remises au même mot, et
// le client aurait raison de crier. C'est aussi cet index qui permet de TIRER
// un code en réessayant sur collision plutôt qu'en interrogeant d'abord.
db.promo_codes.createIndex({ code: 1 }, { unique: true });
// Les codes d'un influenceur ; la liste de la console, par pays et par nature.
db.promo_codes.createIndex({ owner_id: 1, _id: -1 }, { sparse: true });
db.promo_codes.createIndex({ country: 1, kind: 1, _id: -1 });

// ⚠️ UN USAGE PAR (CODE, RÉFÉRENCE) : c'est ce qui rend l'engagement
// IDEMPOTENT. Un rappel de paiement rejoué ou un double clic ne consomment pas
// l'enveloppe deux fois — sans cette contrainte, un budget se vide de moitié
// sur un incident réseau.
db.promo_code_uses.createIndex({ promo_id: 1, ref_id: 1 }, { unique: true });
// La limite PAR PERSONNE se lit ici : c'est elle qui protège de l'abus, pas le
// budget.
db.promo_code_uses.createIndex({ user_id: 1, state: 1 });
// Le suivi d'un code, et la clôture par référence.
db.promo_code_uses.createIndex({ promo_id: 1, _id: -1 });
db.promo_code_uses.createIndex({ ref_id: 1 });

// LES INFLUENCEURS : une fiche par compte, un pseudonyme unique — c'est ce
// qu'on tape pour retrouver quelqu'un.
db.influencers.createIndex({ user_id: 1 }, { unique: true });
db.influencers.createIndex({ handle: 1 }, { unique: true });
db.influencers.createIndex({ country: 1, _id: -1 });
