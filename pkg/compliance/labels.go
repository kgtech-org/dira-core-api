package compliance

// LE NOM D'UNE PIÈCE, EN CLAIR.
//
// ⚠️ ICI, PARCE QUE LA TABLE VIVAIT EN DOUBLE. Les deux verticales portaient la
// même carte `{"licence": "permis de conduire", …}`, recopiée, pour la même
// alerte « pièce à vérifier ». C'est précisément le défaut que cette
// bibliothèque existe pour empêcher : ajouter un type obligeait à le déclarer à
// trois endroits, et le troisième aurait affiché `criminal_record` à un
// exploitant — un identifiant technique dans une notification, sur l'écran de
// quelqu'un qui doit décider vite.
//
// ⚠️ ET LE REPLI EST LE TYPE LUI-MÊME, PAS UN BLANC. Un type inconnu affiche sa
// clé : c'est laid, et c'est exactement ce qu'il faut — un libellé vide ferait
// une alerte qui dit « pièce déposée : » et personne ne saurait laquelle.

// labels : le nom de chaque pièce, par langue.
var labels = map[string]map[string]string{
	"fr": {
		DocLicence:        "permis de conduire",
		DocIDCard:         "pièce d'identité",
		DocCriminalRecord: "casier judiciaire",
		DocSelfie:         "photo du visage",
		DocRegistration:   "carte grise",
		DocInsurance:      "assurance",
		DocInspection:     "contrôle technique",
		DocVehicleFront:   "photo du véhicule — avant",
		DocVehicleRear:    "photo du véhicule — arrière",
		DocVehicleSide:    "photo du véhicule — côté",
	},
	"en": {
		DocLicence:        "driving licence",
		DocIDCard:         "ID document",
		DocCriminalRecord: "criminal record extract",
		DocSelfie:         "face photo",
		DocRegistration:   "vehicle registration",
		DocInsurance:      "insurance",
		DocInspection:     "roadworthiness certificate",
		DocVehicleFront:   "vehicle photo - front",
		DocVehicleRear:    "vehicle photo - rear",
		DocVehicleSide:    "vehicle photo - side",
	},
}

// Label rend le nom d'une pièce dans la langue demandée, le français par
// défaut — les cinq pays ouverts sont francophones.
func Label(kind, locale string) string {
	l := labels["fr"]
	if locale != "" {
		if byLang, ok := labels[locale]; ok {
			l = byLang
		} else if len(locale) > 2 {
			if byLang, ok := labels[locale[:2]]; ok {
				l = byLang
			}
		}
	}
	if name := l[kind]; name != "" {
		return name
	}
	return kind
}
