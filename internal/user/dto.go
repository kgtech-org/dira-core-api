package user

import "time"

// RegisterRequest creates an account. Role admin is not self-assignable:
// only client, driver and merchant are accepted (422 otherwise).
type RegisterRequest struct {
	Phone    string `json:"phone" validate:"required,e164"`
	Name     string `json:"name" validate:"required,min=1,max=120"`
	Password string `json:"password" validate:"required,min=8,max=128"`
	Role     string `json:"role,omitempty" validate:"omitempty,oneof=client driver merchant"`
	Email    string `json:"email,omitempty" validate:"omitempty,email"`
}

// LoginRequest authenticates by phone (clients/drivers — the primary
// identifier in the target market) or by email (back-office/admin accounts).
// Exactly one of the two identifiers is required.
type LoginRequest struct {
	Phone    string `json:"phone,omitempty" validate:"omitempty,e164"`
	Email    string `json:"email,omitempty" validate:"omitempty,email"`
	Password string `json:"password" validate:"required"`
}

// RefreshRequest rotates a refresh token.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// LogoutRequest invalidates a refresh token.
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// UpdateMeRequest updates the caller's profile. Nil fields are unchanged.
type UpdateMeRequest struct {
	Name      *string `json:"name,omitempty" validate:"omitempty,min=1,max=120"`
	FirstName *string `json:"first_name,omitempty" validate:"omitempty,max=80"`
	LastName  *string `json:"last_name,omitempty" validate:"omitempty,max=80"`
	// BirthDate au format `2006-01-02`. Une date SEULE, sans heure ni fuseau :
	// une date de naissance n'a pas d'instant, et l'horodater la décalerait
	// d'un jour selon le fuseau du lecteur.
	BirthDate *string `json:"birth_date,omitempty" validate:"omitempty,len=10"`
	Gender    *string `json:"gender,omitempty" validate:"omitempty,oneof=female male other"`
	Email     *string `json:"email,omitempty" validate:"omitempty,email"`
	AvatarURL *string `json:"avatar_url,omitempty" validate:"omitempty,url"`
}

// UpdatePreferencesRequest règle les préférences. Tout est FACULTATIF, et un
// champ absent reste inchangé : l'écran envoie l'interrupteur qu'on vient de
// basculer, pas les six autres.
type UpdatePreferencesRequest struct {
	OrderUpdates *bool   `json:"order_updates,omitempty"`
	ChatMessages *bool   `json:"chat_messages,omitempty"`
	Promotions   *bool   `json:"promotions,omitempty"`
	Tombola      *bool   `json:"tombola,omitempty"`
	Theme        *string `json:"theme,omitempty" validate:"omitempty,oneof=system light dark"`
	Locale       *string `json:"locale,omitempty" validate:"omitempty,max=16"`
	Sounds       *bool   `json:"sounds,omitempty"`
	// PaymentProvider : l'opérateur mobile money à PRÉSÉLECTIONNER. Vide pour
	// l'oublier. Aucun jeton de paiement n'est conservé — chaque paiement
	// passe par l'opérateur comme la première fois.
	PaymentProvider *string `json:"payment_provider,omitempty" validate:"omitempty,max=40"`
}

// AddressRequest crée ou modifie une adresse enregistrée.
type AddressRequest struct {
	Label   string     `json:"label" validate:"required,max=60"`
	Address string     `json:"address" validate:"required,max=300"`
	Geo     [2]float64 `json:"geo"` // [lng, lat]
	// Details : « portail bleu », « 2e étage, gauche » — ce qui fait trouver
	// la porte et que le client répète à chaque commande.
	Details   string `json:"details" validate:"omitempty,max=300"`
	IsDefault bool   `json:"is_default"`
}

// AddressResponse is a saved address as returned by the API.
type AddressResponse struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	Address   string     `json:"address"`
	Geo       [2]float64 `json:"geo"`
	Details   string     `json:"details,omitempty"`
	IsDefault bool       `json:"is_default,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func newAddressResponse(a *Address) AddressResponse {
	return AddressResponse{
		ID:        a.ID.Hex(),
		Label:     a.Label,
		Address:   a.Address,
		Geo:       a.Geo,
		Details:   a.Details,
		IsDefault: a.IsDefault,
		CreatedAt: a.CreatedAt,
	}
}

// UserResponse is the public representation of a user. It never contains the
// password hash.
type UserResponse struct {
	ID        string    `json:"id"`
	Phone     string    `json:"phone"`
	Name      string    `json:"name"`
	FirstName string    `json:"first_name,omitempty"`
	LastName  string    `json:"last_name,omitempty"`
	BirthDate string    `json:"birth_date,omitempty"` // `2006-01-02`
	Gender    string    `json:"gender,omitempty"`
	Email     string    `json:"email,omitempty"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	// Preferences accompagne le compte : l'application les lit à chaque
	// démarrage, et un appel séparé pour quatre interrupteurs serait un
	// aller-retour de plus sur un réseau mobile.
	Preferences *Preferences `json:"preferences,omitempty"`
}

func newUserResponse(u *User) UserResponse {
	return UserResponse{
		ID:          u.ID.Hex(),
		Phone:       u.Phone,
		Name:        u.Name,
		FirstName:   u.FirstName,
		LastName:    u.LastName,
		BirthDate:   formatBirthDate(u.BirthDate),
		Gender:      u.Gender,
		Email:       u.Email,
		AvatarURL:   u.AvatarURL,
		Preferences: u.Preferences,
		Role:        u.Role,
		Status:      u.Status,
		CreatedAt:   u.CreatedAt,
	}
}

// AuthResponse returns the account plus a fresh token pair.
type AuthResponse struct {
	User         UserResponse `json:"user"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
}

// TokenPairResponse returns a rotated token pair.
type TokenPairResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// formatBirthDate rend la date seule, sans heure : une date de naissance n'a
// pas d'instant, et l'horodater la décalerait d'un jour selon le fuseau du
// lecteur.
func formatBirthDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}
