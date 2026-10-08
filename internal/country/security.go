package country

// LE VERROU DE L'APPLICATION — réglé par PAYS, depuis la console.
//
// Biométrie ou code secret devant l'application : qui ouvre le téléphone de
// quelqu'un n'ouvre pas pour autant son compte Dira. Le verrou vit DANS
// l'application — le serveur ne peut ni le poser ni le vérifier —, mais la
// POLITIQUE, elle, se décide ici : proposé ou imposé, biométrie admise ou code
// seul, au bout de combien de temps on redemande, et combien d'essais ratés
// déconnectent.
//
// ⚠️ POURQUOI PAR PAYS, ET NON PAR COMPTE. Un réglage de compte aurait eu
// l'air plus fin et aurait menti : il se range dans une base, pas dans le
// téléphone resté chez soi, et personne ne peut vérifier à distance qu'un
// appareil est verrouillé. Ce que le pays décide, c'est ce que les
// applications DOIVENT proposer ou imposer — une règle d'exploitation, au même
// titre que le fond de carte ou le plafond de dette. Le choix de la personne,
// lui, vit sur son téléphone.
//
// ⚠️ ET PAR PAYS PLUTÔT QUE GLOBAL parce que les marchés ne se ressemblent
// pas : là où le téléphone est partagé dans la famille, le verrou s'impose ;
// ailleurs il agace. Un réglage global aurait obligé à trancher pour tout le
// monde depuis Lomé.

import (
	"context"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Modes du verrou.
const (
	// LockOff : ne pas proposer de verrou. L'écran de réglages n'affiche
	// rien — mieux qu'une case qui ne fait rien.
	LockOff = "off"
	// LockOptional : la personne choisit. C'est le défaut.
	LockOptional = "optional"
	// LockRequired : l'application EXIGE un verrou avant d'ouvrir le premier
	// écran, et n'offre pas de le retirer.
	//
	// ⚠️ IMPOSER N'EST PAS ANODIN : quelqu'un dont le capteur est cassé et
	// qui oublie son code se retrouve dehors, et la seule sortie est de se
	// reconnecter. C'est pour cela que le code secret est TOUJOURS possible à
	// côté de la biométrie, et que les échecs déconnectent au lieu de bloquer.
	LockRequired = "required"
)

// Défauts, quand le pays n'a rien réglé.
const (
	defaultLockMode         = LockOptional
	defaultLockPINLength    = 4
	defaultLockGraceSeconds = 120
	defaultLockMaxAttempts  = 5
)

// AppLock est la politique de verrou d'un pays.
type AppLock struct {
	// Mode : `off`, `optional` (défaut) ou `required`.
	Mode string `bson:"mode,omitempty"`
	// Biometrics : la biométrie est-elle admise ? Faux = code secret SEUL.
	//
	// ⚠️ Un pointeur, parce que « non réglé » et « refusée » ne sont pas la
	// même chose : un `bool` nu aurait interdit la biométrie dans tout pays
	// enregistré avant ce réglage.
	Biometrics *bool `bson:"biometrics,omitempty"`
	// PINLength : 4 ou 6 chiffres.
	PINLength int `bson:"pin_length,omitempty"`
	// GraceSeconds : combien de temps l'application peut rester en
	// arrière-plan avant de redemander. Zéro = redemander à chaque retour.
	//
	// ⚠️ C'EST LE RÉGLAGE QUI DÉCIDE SI LE VERROU SERA SUPPORTÉ OU CONTOURNÉ.
	// Redemander à chaque bascule — pour lire un SMS de code, pour ouvrir une
	// carte — fait désinstaller l'application ou désactiver le verrou. Deux
	// minutes par défaut.
	GraceSeconds int `bson:"grace_seconds,omitempty"`
	// MaxAttempts : essais ratés avant que l'application ne DÉCONNECTE.
	//
	// ⚠️ DÉCONNECTER, PAS BLOQUER. Un téléphone volé qui se bloque garde un
	// jeton de rafraîchissement valide trente jours ; un téléphone volé qui se
	// déconnecte ne garde rien. Et la personne légitime, elle, retrouve son
	// compte avec un code à usage unique ou son mot de passe.
	MaxAttempts int `bson:"max_attempts,omitempty"`
}

// Sessions borne le nombre d'APPAREILS d'un compte.
//
// ⚠️ NE CONCERNE PAS LES CHAUFFEURS NI LES LIVREURS. Eux n'ont qu'un seul
// appareil, et ce n'est pas un réglage de sécurité : un agent connecté sur deux
// téléphones pousse DEUX flux de positions pour un seul véhicule — le vivier
// voit la même voiture à deux endroits, l'appel part vers le téléphone resté à
// la maison, et la course meurt d'un « personne n'a répondu » que rien
// n'explique. Rendre ce nombre réglable pour eux aurait offert, dans un écran
// d'administration, un bouton qui casse le dispatch sans le dire.
type Sessions struct {
	// MaxDevices : appareils simultanés d'un compte ordinaire — client,
	// marchand, membre du staff. Au-delà, le plus ancien est déconnecté.
	MaxDevices int `bson:"max_devices,omitempty"`
}

// Security rassemble ce qu'un pays décide de la sécurité des applications.
type Security struct {
	AppLock  AppLock  `bson:"app_lock,omitempty"`
	Sessions Sessions `bson:"sessions,omitempty"`
	// SOS : les détections qui proposent le bouton d'alerte, le délai
	// d'annulation, et les numéros de secours du pays. Voir `sos.go`.
	SOS SOS `bson:"sos,omitempty"`
}

// AppLockResponse est la politique telle que la console la lit et que les
// applications la reçoivent — tous les champs posés, aucun à deviner.
type AppLockResponse struct {
	Mode         string `json:"mode"`
	Biometrics   bool   `json:"biometrics"`
	PINLength    int    `json:"pin_length"`
	GraceSeconds int    `json:"grace_seconds"`
	MaxAttempts  int    `json:"max_attempts"`
}

// SessionsResponse est la borne telle que la console la lit et que les
// applications la reçoivent.
type SessionsResponse struct {
	MaxDevices int `json:"max_devices"`
	// AgentMaxDevices est rendu pour être AFFICHÉ, pas réglé : il vaut
	// toujours 1. Le dire évite la question « pourquoi mon chauffeur est-il
	// déconnecté alors que j'ai mis 3 ? ».
	AgentMaxDevices int `json:"agent_max_devices"`
}

// SecurityResponse est le bloc complet.
type SecurityResponse struct {
	AppLock  AppLockResponse  `json:"app_lock"`
	Sessions SessionsResponse `json:"sessions"`
}

// SecurityUpdateRequest règle le verrou et le nombre d'appareils depuis la
// console. Tout est facultatif : on change un champ sans réécrire les autres.
type SecurityUpdateRequest struct {
	// MaxDevices : 1 à 10 appareils par compte ordinaire.
	//
	// ⚠️ Le PLAFOND est volontairement bas. « Autant qu'on veut » aurait
	// laissé un compte partagé par vingt personnes ressembler à un compte
	// ordinaire, et c'est précisément ce que cette borne sert à voir.
	MaxDevices   *int    `json:"max_devices" validate:"omitempty,min=1,max=10"`
	Mode         *string `json:"mode" validate:"omitempty,oneof=off optional required"`
	Biometrics   *bool   `json:"biometrics"`
	PINLength    *int    `json:"pin_length" validate:"omitempty,oneof=4 6"`
	GraceSeconds *int    `json:"grace_seconds" validate:"omitempty,min=0,max=3600"`
	MaxAttempts  *int    `json:"max_attempts" validate:"omitempty,min=3,max=10"`
}

var errNoSecurityUpdate = apperr.Validation(
	"nothing to update: send mode, biometrics, pin_length, grace_seconds, max_attempts or max_devices")

// DefaultMaxDevices : trois appareils par compte ordinaire.
//
// Le téléphone, la tablette, et celui qu'on vient de changer sans penser à se
// déconnecter de l'ancien. Deux auraient fait déconnecter quelqu'un qui n'a
// rien fait de mal ; dix n'auraient rien borné du tout.
const DefaultMaxDevices = 3

// AgentMaxDevices : UN appareil pour un chauffeur ou un livreur. Non réglable
// — voir `Sessions`.
const AgentMaxDevices = 1

func sessionsResponse(s Sessions) SessionsResponse {
	max := s.MaxDevices
	if max <= 0 {
		max = DefaultMaxDevices
	}
	return SessionsResponse{MaxDevices: max, AgentMaxDevices: AgentMaxDevices}
}

// appLockResponse complète les trous avec les défauts : une application ne
// doit jamais avoir à décider ce qu'un champ vide veut dire.
func appLockResponse(l AppLock) AppLockResponse {
	out := AppLockResponse{
		Mode:         l.Mode,
		Biometrics:   true,
		PINLength:    l.PINLength,
		GraceSeconds: l.GraceSeconds,
		MaxAttempts:  l.MaxAttempts,
	}
	if out.Mode == "" {
		out.Mode = defaultLockMode
	}
	if l.Biometrics != nil {
		out.Biometrics = *l.Biometrics
	}
	if out.PINLength == 0 {
		out.PINLength = defaultLockPINLength
	}
	if l.GraceSeconds == 0 && l.MaxAttempts == 0 && l.Mode == "" {
		// Pays jamais réglé : la grâce par défaut. Un pays qui a VOULU zéro
		// garde zéro — c'est pourquoi on ne teste pas `GraceSeconds` seul.
		out.GraceSeconds = defaultLockGraceSeconds
	}
	if out.MaxAttempts == 0 {
		out.MaxAttempts = defaultLockMaxAttempts
	}
	return out
}

// Security rend la politique d'un pays, telle que la console la lit.
func (s *Service) Security(ctx context.Context, code string) (*SecurityResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return &SecurityResponse{
		AppLock:  appLockResponse(inst.Security.AppLock),
		Sessions: sessionsResponse(inst.Security.Sessions),
	}, nil
}

// UpdateSecurity règle le verrou d'un pays.
func (s *Service) UpdateSecurity(ctx context.Context, code string, req SecurityUpdateRequest) (*SecurityResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.Mode == nil && req.Biometrics == nil && req.PINLength == nil &&
		req.GraceSeconds == nil && req.MaxAttempts == nil && req.MaxDevices == nil {
		return nil, errNoSecurityUpdate
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	lock := inst.Security.AppLock
	if req.Mode != nil {
		lock.Mode = *req.Mode
	}
	if req.Biometrics != nil {
		lock.Biometrics = req.Biometrics
	}
	if req.PINLength != nil {
		lock.PINLength = *req.PINLength
	}
	if req.GraceSeconds != nil {
		lock.GraceSeconds = *req.GraceSeconds
	}
	if req.MaxAttempts != nil {
		lock.MaxAttempts = *req.MaxAttempts
	}
	sess := inst.Security.Sessions
	if req.MaxDevices != nil {
		sess.MaxDevices = *req.MaxDevices
	}
	if err := s.repo.SetSecurity(ctx, info.Code, Security{AppLock: lock, Sessions: sess}); err != nil {
		return nil, apperr.Internal(err)
	}
	return &SecurityResponse{AppLock: appLockResponse(lock), Sessions: sessionsResponse(sess)}, nil
}

// MaxDevicesOf rend le nombre d'appareils admis dans un pays — l'adaptateur
// que le module des comptes appelle à chaque nouvelle session.
//
// ⚠️ AU MIEUX : une base muette rend le défaut, jamais une erreur. Refuser une
// connexion parce qu'on n'a pas su lire une borne d'appareils serait hors de
// proportion ; trois est une valeur sûre, et c'est celle qu'on aurait choisie.
func (s *Service) MaxDevicesOf(ctx context.Context, code string) int {
	info, ok := country.Lookup(code)
	if !ok {
		return DefaultMaxDevices
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return DefaultMaxDevices
	}
	return sessionsResponse(inst.Security.Sessions).MaxDevices
}

// AppLockOf rend la politique d'un pays pour les APPLICATIONS — l'adaptateur
// que le module des comptes appelle à la connexion et au rafraîchissement.
//
// ⚠️ AU MIEUX : une base injoignable ou un pays inconnu rendent `nil`, et
// l'application garde le réglage qu'elle avait. Refuser une connexion parce
// qu'on n'a pas su lire une politique de verrou serait disproportionné.
func (s *Service) AppLockOf(ctx context.Context, code string) (mode string, biometrics bool, pinLength, graceSeconds, maxAttempts int, ok bool) {
	info, found := country.Lookup(code)
	if !found {
		return "", false, 0, 0, 0, false
	}
	inst, _, err := s.repo.One(ctx, info.Code)
	if err != nil {
		return "", false, 0, 0, 0, false
	}
	l := appLockResponse(inst.Security.AppLock)
	return l.Mode, l.Biometrics, l.PINLength, l.GraceSeconds, l.MaxAttempts, true
}

// appLockOf rend la politique d'un pays pour le catalogue public.
//
// ⚠️ AU MIEUX, comme `BasemapOf` : une base muette rend les DÉFAUTS, jamais
// une erreur. Faire échouer `GET /countries` — la toute première requête
// d'une application, avant même la connexion — parce qu'un réglage de verrou
// est illisible serait hors de proportion.
func (s *Service) appLockOf(code string) AppLockResponse {
	inst, _, err := s.repo.One(context.Background(), code)
	if err != nil {
		return appLockResponse(AppLock{})
	}
	return appLockResponse(inst.Security.AppLock)
}
