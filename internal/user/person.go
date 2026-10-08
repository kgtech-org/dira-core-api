package user

// L'IDENTITÉ NUE D'UN COMPTE — ce que la politique de confidentialité filtre
// ensuite.
//
// ⚠️ CETTE LECTURE N'EST PAS UNE DIVULGATION, et la distinction porte tout le
// reste. `ContactOf` rend le nom et le téléphone SANS filtre : c'est la vérité
// du back-office, celle que le support doit voir pour traiter une réclamation.
// Ce que deux inconnus s'échangent sur une course, lui, passe par
// `/internal/accounts/disclose`, qui applique la politique du pays — voir
// `internal/country/privacy.go`.
//
// Les confondre serait l'erreur à ne pas faire : une verticale qui appelle la
// mauvaise des deux portes divulgue exactement ce que l'exploitation venait de
// fermer.

import (
	"context"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Person porte les champs d'identité qu'une divulgation peut avoir à filtrer.
//
// ⚠️ PAS D'E-MAIL, PAS DE DATE DE NAISSANCE, PAS D'ADRESSE. Une structure qui
// rend tout finit par être utilisée pour tout : celle-ci ne porte que ce que
// la console peut décider d'ouvrir, et rien d'autre. Un champ ajouté ici doit
// l'être aussi dans `Disclosure`, sinon il traverserait le fil sans que
// personne n'ait réglé s'il devait.
type Person struct {
	ID        string
	Role      string
	Name      string
	FirstName string
	LastName  string
	Phone     string
	AvatarURL string
	Gender    string
	// Country est celui du COMPTE — le repli quand l'appelant ne dit pas dans
	// quel pays se déroule l'opération.
	Country string
}

// PersonOf rend l'identité nue d'un compte.
func (s *Service) PersonOf(ctx context.Context, userID string) (*Person, error) {
	u, err := s.findUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &Person{
		ID: u.ID.Hex(), Role: u.Role, Name: u.Name,
		FirstName: u.FirstName, LastName: u.LastName, Phone: u.Phone,
		AvatarURL: u.AvatarURL, Gender: u.Gender, Country: u.Country,
	}, nil
}

// errPersonNotFound garde le même code que les autres lectures de compte : une
// verticale qui affiche une course n'a pas à distinguer « compte supprimé » de
// « identifiant faux ».
var errPersonNotFound = apperr.NotFound("user_not_found", "user not found")
