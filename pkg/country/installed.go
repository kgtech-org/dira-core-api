package country

// Catalogued sert tout pays du catalogue, avec un pays par défaut.
//
// C'est ce qu'une VERTICALE monte : elle ne tient pas la liste des pays
// ouverts — le socle la tient, en base — et n'a pas besoin de la lire à
// chaque requête. Un administrateur qui demande un pays du catalogue mais
// non ouvert voit des listes vides, ce qui est la bonne réponse.
type Catalogued struct {
	DefaultCode string
}

// Enabled dit si le code est au catalogue.
func (c Catalogued) Enabled(code string) bool { return Known(code) }

// Default rend le pays par défaut du déploiement.
func (c Catalogued) Default() string { return Normalize(c.DefaultCode) }
