// Couche pays v4.2.0 : marque `country: "TG"` sur tout document qui n'en a pas.
// Usage : mongosh ... --eval 'var CORE="dira_core", FOOD="dira_food", VTC="dira_vtc", CODE="TG"' migrate-country.js
var code = typeof CODE === "string" ? CODE : "TG";
var plan = {};
plan[typeof CORE === "string" ? CORE : "dira_core"] = ["users", "token_wallets", "payments", "campaigns", "fleets"];
plan[typeof FOOD === "string" ? FOOD : "dira_food"] = ["merchants", "stores", "orders", "delivery_agents", "vehicles", "deliveries", "promotions", "banners", "feed_videos", "tombola_draws", "tickets", "bug_reports"];
plan[typeof VTC === "string" ? VTC : "dira_vtc"] = ["vtc_rides", "vtc_drivers", "vtc_vehicles", "vtc_cities", "vtc_schedules", "vtc_classes", "vtc_surge_zones"];
for (const dbName of Object.keys(plan)) {
  const d = db.getSiblingDB(dbName);
  const existing = new Set(d.getCollectionNames());
  for (const c of plan[dbName]) {
    if (!existing.has(c)) { print(dbName + "." + c + ": (absent)"); continue; }
    const r = d.getCollection(c).updateMany({ country: { $exists: false } }, { $set: { country: code } });
    print(dbName + "." + c + ": " + r.modifiedCount + " marqués " + code);
  }
}
// Une grille tarifaire par pays : l'index unique sur `key` seul cède la place à (country, key).
try {
  const vtc = db.getSiblingDB(typeof VTC === "string" ? VTC : "dira_vtc");
  if (vtc.getCollectionNames().includes("vtc_classes")) { vtc.getCollection("vtc_classes").dropIndex("key_1"); print("vtc_classes: index key_1 retiré"); }
} catch (e) { print("vtc_classes: key_1 " + e.message); }
