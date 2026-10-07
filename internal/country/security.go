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

// Security rassemble ce qu'un pays décide de la sécurité des applications.
// Un seul bloc aujourd'hui ; d'autres viendront s'y ranger.
type Security struct {
	AppLock AppLock `bson:"app_lock,omitempty"`
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

// SecurityResponse est le bloc complet.
type SecurityResponse struct {
	AppLock AppLockResponse `json:"app_lock"`
}

// SecurityUpdateRequest règle le verrou depuis la console. Tout est
// facultatif : on change un champ sans réécrire les autres.
type SecurityUpdateRequest struct {
	Mode         *string `json:"mode" validate:"omitempty,oneof=off optional required"`
	Biometrics   *bool   `json:"biometrics"`
	PINLength    *int    `json:"pin_length" validate:"omitempty,oneof=4 6"`
	GraceSeconds *int    `json:"grace_seconds" validate:"omitempty,min=0,max=3600"`
	MaxAttempts  *int    `json:"max_attempts" validate:"omitempty,min=3,max=10"`
}

var errNoSecurityUpdate = apperr.Validation(
	"nothing to update: send mode, biometrics, pin_length, grace_seconds or max_attempts")

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
	return &SecurityResponse{AppLock: appLockResponse(inst.Security.AppLock)}, nil
}

// UpdateSecurity règle le verrou d'un pays.
func (s *Service) UpdateSecurity(ctx context.Context, code string, req SecurityUpdateRequest) (*SecurityResponse, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.Mode == nil && req.Biometrics == nil && req.PINLength == nil &&
		req.GraceSeconds == nil && req.MaxAttempts == nil {
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
	if err := s.repo.SetSecurity(ctx, info.Code, Security{AppLock: lock}); err != nil {
		return nil, apperr.Internal(err)
	}
	return &SecurityResponse{AppLock: appLockResponse(lock)}, nil
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
