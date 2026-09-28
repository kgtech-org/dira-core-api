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
	// Prénom et nom, FACULTATIFS : `name` reste le nom d'affichage. Les
	// applications les demandent à l'inscription (spécification projet
	// §4.4) et la fiche les porte déjà (`PATCH /me`) ; les refuser ici
	// obligeait à un second appel — ou, pire, à les taire.
	FirstName string `json:"first_name,omitempty" validate:"omitempty,max=80"`
	LastName  string `json:"last_name,omitempty" validate:"omitempty,max=80"`
	// App, DeviceID et DeviceName : l'inscription OUVRE une session, et un
	// livreur qui vient de créer son compte est déjà connecté sur ce
	// téléphone. Sans eux, sa toute première session serait la seule à
	// n'être bornée à aucun appareil — et elle peut durer trente jours.
	App        string `json:"app,omitempty" validate:"omitempty,oneof=client driver merchant console"`
	DeviceID   string `json:"device_id,omitempty" validate:"omitempty,max=128"`
	DeviceName string `json:"device_name,omitempty" validate:"omitempty,max=120"`
}

// LoginRequest authenticates by phone (clients/drivers — the primary
// identifier in the target market) or by email (back-office/admin accounts).
// Exactly one of the two identifiers is required.
type LoginRequest struct {
	Phone    string `json:"phone,omitempty" validate:"omitempty,e164"`
	Email    string `json:"email,omitempty" validate:"omitempty,email"`
	Password string `json:"password" validate:"required"`
	// App dit QUELLE APPLICATION demande, et c'est elle qui décide si ce
	// compte a le droit d'entrer ici.
	//
	// ⚠️ LE SERVEUR NE PEUT PAS LE DEVINER. Sans ce mot, un client se
	// connectait dans l'application chauffeur : le mot de passe est bon, le
	// jeton est émis — puis chaque écran répond 403, et la personne croit
	// l'application cassée plutôt que de comprendre qu'elle s'est trompée
	// d'application.
	//
	// ABSENT = aucune vérification, le comportement d'avant : une
	// application pas encore mise à jour continue de fonctionner.
	App string `json:"app,omitempty" validate:"omitempty,oneof=client driver merchant console"`
	// DeviceID est l'IDENTIFIANT D'INSTALLATION de l'application : un chauffeur
	// ou un livreur ne tient qu'UNE session, et c'est ce champ qui dit laquelle.
	//
	// ⚠️ LE SERVEUR NE PEUT PAS LE DEVINER. Ni l'adresse IP (un chauffeur en
	// change dix fois par jour), ni le modèle du téléphone (toute une ville
	// roule sur le même Tecno), ni le jeton de notification (il change à
	// chaque réinstallation du service Google) ne désignent une installation.
	// Sans ce champ, deux téléphones du même compte sont indiscernables — et
	// c'est exactement la situation qu'on veut faire cesser : deux flux de
	// positions pour une seule voiture.
	//
	// ⚠️ IL DOIT SURVIVRE AUX REDÉMARRAGES ET VIVRE AUSSI LONGTEMPS QUE LE
	// JETON DE RAFRAÎCHISSEMENT, à côté de lui. Tiré à chaque lancement, ou
	// rangé dans un cache que le système nettoie, l'application se chasse
	// ELLE-MÊME à chaque ouverture : « vous avez été déconnecté » en boucle,
	// sur un seul téléphone.
	//
	// ABSENT = aucune règle d'appareil, le comportement d'avant : une
	// application pas encore mise à jour continue de fonctionner.
	DeviceID string `json:"device_id,omitempty" validate:"omitempty,max=128"`
	// DeviceName est le libellé LISIBLE de l'appareil — « Tecno Spark 10 ·
	// Android 13 ». Il ne sert qu'à une chose, et elle compte : que l'écran
	// puisse dire « vous vous êtes connecté sur Tecno Spark 10 » au lieu de
	// « erreur de connexion ». Un refus qui ne nomme pas l'appareil laisse
	// croire à une panne, et le support reçoit l'appel.
	DeviceName string `json:"device_name,omitempty" validate:"omitempty,max=120"`
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
	// Country est le pays du compte — celui que l'application envoie dans
	// `X-Dira-Country`. CountryAny dit que ce compte peut regarder un AUTRE
	// pays par cet en-tête : la direction, sur la console. Absent pour tout
	// le monde d'autre.
	Country    string `json:"country,omitempty"`
	CountryAny bool   `json:"country_any,omitempty"`
	// Scopes sont les PORTÉES d'un membre du staff — `core`, `vtc`, `food`
	// — telles que les verticales les vérifient. La console les lit pour ne
	// proposer que les guichets que la personne couvre : un support « courses »
	// n'a pas à voir un onglet « livraison » qui lui répondra 403. Absent pour
	// tout le monde d'autre.
	Scopes []string `json:"scopes,omitempty"`
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
		Country:     u.Country,
		CreatedAt:   u.CreatedAt,
	}
}

// AuthResponse returns the account plus a fresh token pair.
type AuthResponse struct {
	User         UserResponse `json:"user"`
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	// Session décrit l'appareil qui vient de prendre la session, et celui
	// qu'il a chassé. ABSENT pour tout compte qui a le droit d'être sur
	// plusieurs appareils — un client, un marchand, un membre du staff — et
	// absent aussi quand l'application n'a pas déclaré son appareil.
	Session *SessionResponse `json:"session,omitempty"`
}

// SessionResponse dit à l'application où sa session est ouverte.
//
// ⚠️ ELLE EXISTE POUR QUE LE NOUVEAU TÉLÉPHONE PUISSE PRÉVENIR. Celui qui a été
// chassé est peut-être éteint, ou sans réseau, et n'apprendra la nouvelle que
// dans trois jours ; l'instant de la connexion est donc la seule occasion de
// dire à quelqu'un que son compte servait ailleurs. Un chauffeur dont le compte
// est utilisé par un tiers le découvre ici.
type SessionResponse struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name,omitempty"`
	// SingleDevice annonce la RÈGLE : ce compte ne tient qu'une session.
	// L'application s'en sert pour afficher l'avertissement au bon public
	// sans coder en dur « si je suis l'application chauffeur ».
	SingleDevice bool `json:"single_device"`
	// SupersededDeviceID / Name : l'appareil que CETTE connexion vient de
	// chasser. Absents quand il n'y avait personne à chasser — le cas normal.
	SupersededDeviceID   string `json:"superseded_device_id,omitempty"`
	SupersededDeviceName string `json:"superseded_device_name,omitempty"`
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
