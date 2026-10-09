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
	//
	// ⚠️ `partner` N'EST PAS DANS CETTE LISTE, délibérément : `Register` refuse
	// ce rôle — il donne des pouvoirs sur le travail d'autres personnes —, donc
	// l'accepter ici n'ouvrirait rien et ferait croire le contraire. L'accès
	// d'un partenaire s'ouvre depuis la fiche de sa flotte.
	App        string `json:"app,omitempty" validate:"omitempty,oneof=client driver courier merchant console"`
	DeviceID   string `json:"device_id,omitempty" validate:"omitempty,max=128"`
	DeviceName string `json:"device_name,omitempty" validate:"omitempty,max=120"`
	// Platform dit SUR QUOI l'application tourne — `web`, `android`, `ios`.
	//
	// ⚠️ ACCEPTÉE ET IGNORÉE depuis la v4.40.0. Elle ne servait qu'à choisir la
	// clé Google du pays ; le serveur n'en garde plus aucune, et le fond par
	// défaut est le même pour les trois plateformes.
	//
	// ⚠️ ELLE RESTE DÉCLARÉE, ET IL NE FAUT PAS LA RETIRER. Le décodeur refuse
	// les champs inconnus (`422 unknown_field`) : la supprimer d'ici casserait,
	// d'un déploiement à l'autre, TOUTES les applications qui l'envoient déjà —
	// c'est-à-dire toutes. Un champ toléré coûte une ligne ; un 422 à la
	// connexion coûte la journée.
	Platform string `json:"platform,omitempty" validate:"omitempty,oneof=web android ios"`
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
	// ⚠️ DEUX MOTS POUR LES DEUX MÉTIERS D'AGENT : `driver` est
	// l'application des COURSES, `courier` celle de la LIVRAISON. Les deux
	// envoyaient `driver`, et tant qu'elles le faisaient le socle ne pouvait
	// pas les distinguer : un chauffeur qui ouvrait la livraison recevait un
	// jeton parfaitement valide, se faisait poser un profil de livreur au
	// passage, et chassait sa propre session de courses de son propre
	// téléphone — voir `agentapp.go`.
	//
	// ⚠️ LE SERVEUR NE PEUT PAS LE DEVINER. Sans ce mot, un client se
	// connectait dans l'application chauffeur : le mot de passe est bon, le
	// jeton est émis — puis chaque écran répond 403, et la personne croit
	// l'application cassée plutôt que de comprendre qu'elle s'est trompée
	// d'application.
	//
	// ABSENT = aucune vérification, le comportement d'avant : une
	// application pas encore mise à jour continue de fonctionner.
	// ⚠️⚠️ `partner` EST DANS CETTE LISTE, et il a failli ne pas y être. Le
	// rôle existait, la table `appRole` le connaissait, la console partenaire
	// l'envoyait — et la validation du corps le refusait par un `422` sur le
	// champ `app` : un rôle complet, sans aucune porte. Ça ne s'est vu qu'en
	// ouvrant la console pour de vrai. Voir `TestEveryKnownAppCanLogIn`, qui
	// fige l'accord entre cette liste et `appRole`.
	App string `json:"app,omitempty" validate:"omitempty,oneof=client driver courier merchant partner console"`
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
	// Platform dit SUR QUOI l'application tourne — `web`, `android`, `ios`.
	//
	// ⚠️ ACCEPTÉE ET IGNORÉE depuis la v4.40.0. Elle ne servait qu'à choisir la
	// clé Google du pays ; le serveur n'en garde plus aucune, et le fond par
	// défaut est le même pour les trois plateformes.
	//
	// ⚠️ ELLE RESTE DÉCLARÉE, ET IL NE FAUT PAS LA RETIRER. Le décodeur refuse
	// les champs inconnus (`422 unknown_field`) : la supprimer d'ici casserait,
	// d'un déploiement à l'autre, TOUTES les applications qui l'envoient déjà —
	// c'est-à-dire toutes. Un champ toléré coûte une ligne ; un 422 à la
	// connexion coûte la journée.
	Platform string `json:"platform,omitempty" validate:"omitempty,oneof=web android ios"`
}

// RefreshRequest rotates a refresh token.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
	// Platform : ACCEPTÉE ET IGNORÉE depuis la v4.40.0 — voir
	// `RegisterRequest.Platform`. À ne pas retirer : le décodeur refuse les
	// champs inconnus, et toutes les applications l'envoient.
	//
	// Le fond de carte, lui, est servi au rafraîchissement QUOI QU'IL ARRIVE :
	// un chauffeur reste connecté trente jours, et un défaut changé dans la
	// console doit l'atteindre avant.
	Platform string `json:"platform,omitempty" validate:"omitempty,oneof=web android ios"`
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
	// EraseAt est la date à laquelle l'identité d'un compte FERMÉ s'en va —
	// absente pour tout autre compte. Voir `erasure.go`.
	//
	// ⚠️ À AFFICHER PARTOUT OÙ `status: closed` APPARAÎT, application comme
	// console. « Fermé » tout seul ne dit pas si c'est réversible ; « fermé,
	// effacement le 6 novembre » dit à la fois ce qui va se passer et de
	// combien de temps dispose le support pour l'annuler.
	EraseAt *time.Time `json:"erase_at,omitempty"`
	// AnonymisedAt dit que l'identité EST partie : ce compte ne se rouvre plus,
	// et son nom n'est plus un nom. Sans ce champ, un écran proposerait
	// « réactiver » sur une coquille vide, et le refus arriverait après le clic.
	AnonymisedAt *time.Time `json:"anonymised_at,omitempty"`
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
		// `anonymised_at` ne demande aucun réglage : c'est une date écrite sur
		// la ligne. `erase_at`, lui, est CALCULÉ depuis le délai de grâce du
		// déploiement — voir `Service.userResponse`.
		AnonymisedAt: u.AnonymisedAt,
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
	// Maps dit quel fond de carte afficher, et porte la clé Google de cette
	// plateforme quand le pays en a configuré une. ABSENT quand la plateforme
	// ne s'est pas nommée — l'application garde alors notre fond.
	Maps *MapsResponse `json:"maps,omitempty"`
	// AppLock est la politique de VERROU du pays — biométrie ou code secret
	// devant l'application. ABSENTE quand le pays n'est pas connu ou que le
	// module n'est pas branché : l'application garde alors son réglage.
	AppLock *AppLockResponse `json:"app_lock,omitempty"`
	// Created : ce compte VIENT DE NAÎTRE, à la vérification d'un code.
	//
	// ⚠️ C'EST LE SEUL SIGNAL QUI DIT À L'APPLICATION DE DEMANDER LE NOM. La
	// demande de code, elle, ne dit jamais si le numéro est connu — ce serait
	// un annuaire (voir `otp.go`) —, donc l'écran « comment vous
	// appelez-vous ? » ne peut se décider qu'ici, une fois la personne
	// authentifiée.
	Created bool `json:"created,omitempty"`
}

// ErasureRequest demande la suppression de SON compte.
//
// ⚠️ ELLE REDEMANDE DE PROUVER QUI ON EST, et c'est le seul champ qu'elle
// porte : l'opération est irréversible, et un téléphone déverrouillé posé sur
// une table suffirait sinon à faire disparaître le compte de quelqu'un.
type ErasureRequest struct {
	// Password : pour un compte qui en a un.
	Password string `json:"password,omitempty" validate:"omitempty,max=128"`
	// Code : pour un compte né par code — il en redemande un
	// (`POST /auth/otp`), et celui-ci est consommé.
	Code string `json:"code,omitempty" validate:"omitempty,min=4,max=12"`
}

// ErasureResponse dit ce qui a été fait, et quand l'identité partira.
type ErasureResponse struct {
	// Status vaut `closed` : le compte ne se connecte plus dès maintenant.
	Status string `json:"status"`
	// EraseAt est la date à laquelle le nom, le téléphone et les adresses
	// s'en vont. ⚠️ À AFFICHER : « votre compte est fermé, vos données seront
	// effacées le 6 novembre » est une phrase qui se comprend ; « compte
	// supprimé » suivi d'un historique encore lisible chez le support ne se
	// comprend pas.
	EraseAt time.Time `json:"erase_at"`
}

// OTPRequest demande un code à usage unique pour ce numéro.
type OTPRequest struct {
	Phone string `json:"phone" validate:"required,e164"`
	// Channel : `whatsapp` (défaut) ou `sms`. Le serveur rend le canal qui a
	// RÉELLEMENT servi — un fournisseur peut basculer.
	Channel string `json:"channel,omitempty" validate:"omitempty,oneof=whatsapp sms"`
	// App : seules les applications de CLIENT ouvrent cette porte. Vide =
	// toléré, comme ailleurs, pour une application pas encore à jour.
	App    string `json:"app,omitempty" validate:"omitempty,oneof=client driver courier merchant console"`
	Locale string `json:"locale,omitempty" validate:"omitempty,oneof=fr en"`
}

// OTPRequestResponse dit ce qui est parti, et quand on pourra redemander.
type OTPRequestResponse struct {
	Sent    bool   `json:"sent"`
	Channel string `json:"channel"`
	// ExpiresAt : le compte à rebours de l'écran se rend DEPUIS CETTE DATE,
	// jamais depuis l'horloge du téléphone.
	ExpiresAt time.Time `json:"expires_at"`
	// ResendAfter est le nombre de secondes avant qu'un nouveau code puisse
	// être demandé (`429 otp_too_soon` avant).
	ResendAfter int `json:"resend_after"`
	// DevCode est le code EN CLAIR, et il n'existe que tant qu'aucune
	// passerelle n'est câblée (`channel: "echo"`). Il disparaîtra sans
	// préavis : une application qui le lit doit le traiter comme un bonus de
	// développement, jamais comme un dû.
	DevCode string `json:"dev_code,omitempty"`
}

// OTPVerifyRequest consomme le code : elle inscrit ou connecte.
type OTPVerifyRequest struct {
	Phone string `json:"phone" validate:"required,e164"`
	Code  string `json:"code" validate:"required,min=4,max=12"`
	// Name, FirstName, LastName : FACULTATIFS, et lus seulement si le compte
	// naît ici. Sans nom, le numéro fait office de nom d'affichage et la
	// personne le changera depuis son profil.
	Name       string `json:"name,omitempty" validate:"omitempty,max=120"`
	FirstName  string `json:"first_name,omitempty" validate:"omitempty,max=80"`
	LastName   string `json:"last_name,omitempty" validate:"omitempty,max=80"`
	App        string `json:"app,omitempty" validate:"omitempty,oneof=client driver courier merchant console"`
	DeviceID   string `json:"device_id,omitempty" validate:"omitempty,max=128"`
	DeviceName string `json:"device_name,omitempty" validate:"omitempty,max=120"`
	// Platform : acceptée et ignorée, comme à l'inscription — voir
	// `RegisterRequest.Platform`. La retirer ferait un `422 unknown_field`
	// chez toutes les applications qui l'envoient déjà.
	Platform string `json:"platform,omitempty" validate:"omitempty,oneof=web android ios"`
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
	// Maps : le fond de carte À JOUR. C'est ici que la rotation d'une clé
	// atteint une application déjà connectée, sans attendre sa reconnexion.
	Maps *MapsResponse `json:"maps,omitempty"`
	// AppLock : la politique de VERROU à jour, pour la même raison.
	AppLock *AppLockResponse `json:"app_lock,omitempty"`
}

// AppLockResponse est la politique de verrou servie à une application.
//
// ⚠️ ELLE DIT CE QU'IL FAUT PROPOSER OU IMPOSER, PAS CE QUI EST POSÉ. Le
// verrou vit dans le téléphone ; le serveur ne peut pas vérifier qu'il y est,
// et une application qui l'ignorerait ne serait pas refusée. C'est une
// politique d'exploitation, pas un contrôle d'accès — ne jamais faire reposer
// la sécurité d'une donnée sur elle : ce qui protège vraiment, c'est le jeton
// en stockage sécurisé et sa durée de vie.
type AppLockResponse struct {
	// Mode : `off` (ne rien proposer), `optional` (la personne choisit),
	// `required` (l'application exige un verrou avant son premier écran).
	Mode string `json:"mode"`
	// Biometrics : la biométrie est-elle admise ? Faux = code secret SEUL.
	// ⚠️ Le code secret reste TOUJOURS possible à côté de la biométrie :
	// un capteur cassé ne doit pas enfermer quelqu'un dehors.
	Biometrics bool `json:"biometrics"`
	// PINLength : 4 ou 6 chiffres.
	PINLength int `json:"pin_length"`
	// GraceSeconds : temps en arrière-plan avant de redemander. Zéro =
	// redemander à chaque retour.
	GraceSeconds int `json:"grace_seconds"`
	// MaxAttempts : essais ratés avant que l'application ne DÉCONNECTE —
	// elle ne bloque pas. Un téléphone volé qui se bloque garde un jeton de
	// rafraîchissement valide trente jours ; déconnecté, il ne garde rien.
	MaxAttempts int `json:"max_attempts"`
}

// MapsResponse est le fond de carte servi à une application : UN SEUL CHAMP.
//
// ⚠️ AUCUNE CLÉ N'Y VOYAGE (v4.40.0). Le serveur en a servi une, par pays et
// par plateforme, du 22 au 29 septembre 2026 — pour la faire tourner sans
// republier. Inutilisable en pratique : chaque application porte déjà SA clé,
// restreinte à son empreinte, à son bundle ou à son référent, et c'est cette
// restriction qui protège une clé. Le serveur ne répond donc qu'à la question
// qu'il est seul à pouvoir trancher : quel fond montrer D'ABORD ici.
type MapsResponse struct {
	// Basemap est le fond par DÉFAUT du pays — la case cochée d'avance, que la
	// personne peut changer, et dont son choix a le dernier mot.
	Basemap string `json:"basemap"`
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
