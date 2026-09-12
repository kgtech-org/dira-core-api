package notify

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
)

// Pusher porte une notification jusqu'aux téléphones. Interface définie ICI,
// côté consommateur : le module ne connaît pas Firebase, il connaît « envoyer
// un titre et un corps à des jetons ».
type Pusher interface {
	Push(ctx context.Context, msgs []PushMessage) ([]PushResult, error)
}

// PushMessage est une notification pour UN appareil.
type PushMessage struct {
	Token string
	Title string
	Body  string
	Data  map[string]string
	// DataOnly : pas de notification système, seulement des données — voir
	// Signal. TTL borne la vie du message chez FCM.
	DataOnly bool
	TTL      time.Duration
}

// PushResult dit ce qu'il est advenu d'un envoi.
type PushResult struct {
	Token string
	Err   error
	// Unregistered : l'appareil ne reviendra pas (application désinstallée,
	// jeton remplacé). À distinguer d'une panne — un jeton mort ne guérit pas.
	Unregistered bool
}

// Accounts rend les réglages d'un compte : la langue CHOISIE dans
// l'application, et l'autorisation d'une catégorie de notification.
//
// Les deux en un appel, parce qu'ils se demandent ensemble, juste avant
// d'envoyer : deux lectures du même document n'apporteraient rien.
//
// Facultatif. Sans lui, tout est autorisé et seule la langue de l'appareil
// compte — c'est-à-dire le comportement d'avant les préférences.
type Accounts interface {
	NotificationPrefs(ctx context.Context, userID, category string) (locale string, allowed bool)
}

type Service struct {
	repo   *Repository
	pusher Pusher
	users  Accounts

	// cache des gabarits : une notification par commande lirait sinon la même
	// ligne à chaque changement de statut.
	mu       sync.RWMutex
	cache    map[string]Template
	cachedAt time.Time
}

// cacheTTL : un gabarit modifié au back-office s'applique dans la minute.
// Assez court pour qu'un exploitant voie son effet, assez long pour ne pas
// relire la base à chaque notification.
const cacheTTL = 60 * time.Second

func NewService(repo *Repository) *Service {
	return &Service{repo: repo, cache: map[string]Template{}}
}

// SetPusher branche le transport (câblage). Nil = aucune notification n'est
// envoyée, et c'est dit une fois au démarrage, pas à chaque message.
func (s *Service) SetPusher(p Pusher) { s.pusher = p }

// SetAccounts branche les réglages de compte (câblage).
func (s *Service) SetAccounts(a Accounts) { s.users = a }

// --- appareils ---

// RegisterDevice enregistre le téléphone d'un utilisateur.
func (s *Service) RegisterDevice(ctx context.Context, userID string, req RegisterDeviceRequest) error {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return apperr.Validation("invalid user id").WithCause(err)
	}
	return s.repo.UpsertDevice(ctx, &Device{
		UserID:   uid,
		Token:    req.Token,
		Platform: req.Platform,
		Locale:   normalizeLocale(req.Locale),
	})
}

// UnregisterDevice retire un téléphone (déconnexion).
func (s *Service) UnregisterDevice(ctx context.Context, userID, token string) error {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return apperr.Validation("invalid user id").WithCause(err)
	}
	return s.repo.DeleteDevice(ctx, uid, token)
}

// --- envoi ---

// Notify rend un gabarit et l'envoie aux appareils d'un utilisateur.
//
// AU MIEUX, et sans erreur en retour : aucune opération métier ne doit
// échouer parce qu'une notification n'est pas partie. Une commande acceptée
// dont le client n'a pas été prévenu reste une commande acceptée.
func (s *Service) Notify(ctx context.Context, userID, key string, vars map[string]string, data map[string]string) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return
	}
	tmpl, ok := s.template(ctx, key)
	if !ok || !tmpl.Enabled {
		return
	}

	category := categoryOf(key)
	accountLocale := ""
	if s.users != nil {
		var allowed bool
		accountLocale, allowed = s.users.NotificationPrefs(ctx, userID, category)
		// Une catégorie coupée n'est ni poussée ni ARCHIVÉE : la garder dans
		// le centre de notifications ferait réapparaître dans une liste ce
		// que l'utilisateur a explicitement refusé de voir.
		//
		// L'appel de course fait exception : c'est le gagne-pain du livreur,
		// et un réglage mal compris le rendrait invisible du dispatch sans
		// qu'il sache pourquoi.
		if !allowed && Muteable(category) {
			return
		}
	}

	devices, err := s.repo.ActiveDevices(ctx, uid)
	if err != nil {
		slog.WarnContext(ctx, "notify: devices unavailable", "user_id", userID, "error", err)
		devices = nil
	}

	// La langue de l'ARCHIVE est celle du compte : la liste se relit des
	// jours plus tard, et un appareil changé entre-temps ne doit pas la
	// laisser dans une langue que son propriétaire n'a pas choisie.
	archived := pick(tmpl, accountLocale)
	archTitle, _ := Render(archived.Title, vars)
	archBody, _ := Render(archived.Body, vars)
	if err := s.repo.InsertInbox(ctx, &Inbox{
		UserID: uid, Key: key, Category: category,
		Title:     Truncate(archTitle, MaxTitle),
		Body:      Truncate(archBody, MaxBody),
		Data:      data,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		// AU MIEUX : perdre l'archive ne doit pas empêcher la notification
		// de partir. C'est l'inverse qui serait grave.
		slog.WarnContext(ctx, "notify: inbox entry not stored", "key", key, "error", err)
	}

	if s.pusher == nil || len(devices) == 0 {
		// Rien à pousser — mais l'archive, elle, est écrite : c'est ce qui
		// permet à un utilisateur qui rouvre l'application de retrouver ce
		// qu'il aurait manqué, téléphone éteint.
		return
	}

	msgs := make([]PushMessage, 0, len(devices))
	for _, d := range devices {
		// La langue de l'APPAREIL prime sur celle du compte : c'est celle de
		// l'écran que la personne a sous les yeux.
		text := pick(tmpl, firstNonEmpty(d.Locale, normalizeLocale(accountLocale)))
		title, missTitle := Render(text.Title, vars)
		body, missBody := Render(text.Body, vars)
		if len(missTitle)+len(missBody) > 0 {
			// Le message part quand même — choix produit. Mais le trou doit
			// se voir ICI, dans les journaux, et pas seulement chez le
			// client.
			slog.WarnContext(ctx, "notify: template rendered with empty variables",
				"key", key, "missing", append(missTitle, missBody...))
		}
		msgs = append(msgs, PushMessage{
			Token: d.Token,
			Title: Truncate(title, MaxTitle),
			Body:  Truncate(body, MaxBody),
			Data:  data,
		})
	}

	results, err := s.pusher.Push(ctx, msgs)
	if err != nil {
		// Panne de transport : AUCUN appareil n'est en cause, et il ne faut
		// surtout pas les marquer morts.
		slog.WarnContext(ctx, "notify: push failed", "key", key, "error", err)
		return
	}
	for _, r := range results {
		switch {
		case r.Unregistered:
			// Un jeton mort ne guérit pas. Le garder joignable ferait
			// réessayer à chaque notification, indéfiniment.
			if err := s.repo.DisableDevice(ctx, r.Token, "unregistered"); err != nil {
				slog.WarnContext(ctx, "notify: device not disabled", "error", err)
			}
		case r.Err != nil:
			slog.WarnContext(ctx, "notify: push rejected", "key", key, "error", r.Err)
		}
	}
}

// --- gabarits ---

// template rend le gabarit d'une clé : celui de la base, sinon le compilé.
func (s *Service) template(ctx context.Context, key string) (Template, bool) {
	s.mu.RLock()
	fresh := time.Since(s.cachedAt) < cacheTTL
	t, hit := s.cache[key]
	s.mu.RUnlock()
	if fresh && hit {
		return t, true
	}
	if stored, err := s.repo.FindTemplate(ctx, key); err == nil && stored != nil {
		s.mu.Lock()
		s.cache[key] = *stored
		s.cachedAt = time.Now()
		s.mu.Unlock()
		return *stored, true
	} else if err != nil {
		// Base illisible : on sert le gabarit compilé plutôt que de ne rien
		// envoyer. Un texte non personnalisé vaut mieux qu'un silence.
		slog.WarnContext(ctx, "notify: template unreadable, using compiled default", "key", key, "error", err)
	}
	d, ok := defaults[key]
	return d, ok
}

// ListTemplates rend tous les gabarits : ceux de la base, complétés par les
// compilés que personne n'a encore modifiés.
//
// La fusion se fait ICI et non à l'écriture : amorcer la base au démarrage
// figerait les textes compilés, et une correction livrée dans une version
// suivante ne s'appliquerait jamais.
func (s *Service) ListTemplates(ctx context.Context) ([]TemplateResponse, error) {
	stored, err := s.repo.ListTemplates(ctx)
	if err != nil {
		return nil, err
	}
	byKey := map[string]Template{}
	for _, d := range Defaults() {
		byKey[d.Key] = d
	}
	custom := map[string]bool{}
	for _, t := range stored {
		byKey[t.Key] = t
		custom[t.Key] = true
	}
	out := make([]TemplateResponse, 0, len(byKey))
	for _, t := range byKey {
		out = append(out, toTemplateResponse(t, custom[t.Key]))
	}
	sortByKey(out)
	return out, nil
}

// SaveTemplate écrit un gabarit et vide le cache.
func (s *Service) SaveTemplate(ctx context.Context, key string, req SaveTemplateRequest) (*TemplateResponse, error) {
	if _, known := defaults[key]; !known {
		// Clés FERMÉES : un gabarit sous une clé que le code n'émet jamais ne
		// partirait nulle part, et l'exploitant croirait avoir configuré
		// quelque chose.
		return nil, apperr.New("unknown_template", "no message is sent under this key", 422)
	}
	if len(req.Locales) == 0 {
		return nil, apperr.Validation("at least one locale is required")
	}
	fr, ok := req.Locales[LocaleFR]
	if !ok || fr.Body == "" {
		// Le français est le REPLI : sans lui, un destinataire dont la langue
		// manque n'aurait rien à recevoir.
		return nil, apperr.New("missing_fallback_locale", "the fr locale is the fallback and cannot be empty", 422)
	}
	locales := map[string]Text{}
	for loc, txt := range req.Locales {
		loc = normalizeLocale(loc)
		if loc == "" || txt.Body == "" {
			continue
		}
		if len([]rune(txt.Title)) > MaxTitle || len([]rune(txt.Body)) > MaxBody {
			return nil, apperr.Validation("title or body is too long")
		}
		// Une variable que la plateforme ne remplit jamais rendrait du VIDE à
		// chaque envoi, définitivement. Personne n'écrit `[store_nom]` en le
		// sachant : on le découvre sur le téléphone d'un client, des semaines
		// plus tard. C'est ici, et seulement ici, que cela se corrige.
		if unknown := unknownVariables(key, txt.Title, txt.Body); len(unknown) > 0 {
			return nil, apperr.New("unknown_variable",
				"this message never provides: "+strings.Join(unknown, ", "), 422).
				WithMeta(map[string]any{"unknown": unknown, "available": Provided(key)})
		}
		locales[loc] = txt
	}
	t := &Template{
		Key:         key,
		Locales:     locales,
		Description: req.Description,
		Enabled:     req.Enabled,
	}
	if t.Description == "" {
		t.Description = defaults[key].Description
	}
	if err := s.repo.SaveTemplate(ctx, t); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache = map[string]Template{}
	s.mu.Unlock()

	resp := toTemplateResponse(*t, true)
	return &resp, nil
}

// Preview rend un gabarit avec des valeurs, sans rien envoyer.
//
// L'écran d'administration en a besoin : un exploitant doit voir la phrase
// finale avant qu'elle ne parte à des milliers de téléphones.
func (s *Service) Preview(ctx context.Context, key, locale string, vars map[string]string) (*PreviewResponse, error) {
	t, ok := s.template(ctx, key)
	if !ok {
		return nil, apperr.NotFound("template_not_found", "template not found")
	}
	text := pick(t, normalizeLocale(locale))
	title, missTitle := Render(text.Title, vars)
	body, missBody := Render(text.Body, vars)
	return &PreviewResponse{
		Title:   Truncate(title, MaxTitle),
		Body:    Truncate(body, MaxBody),
		Missing: mergeSorted(missTitle, missBody),
	}, nil
}

// pick choisit la langue : celle demandée, sinon le repli français.
//
// Le repli est INCONDITIONNEL : un gabarit traduit à moitié doit partir en
// français plutôt que de ne pas partir.
func pick(t Template, locale string) Text {
	if txt, ok := t.Locales[locale]; ok && txt.Body != "" {
		return txt
	}
	if txt, ok := t.Locales[DefaultLocale]; ok {
		return txt
	}
	for _, txt := range t.Locales {
		return txt // dernier recours : n'importe quelle langue vaut mieux que rien
	}
	return Text{}
}

// normalizeLocale ramène « fr-FR », « FR_fr » à « fr ». Un appareil déclare sa
// langue dans une demi-douzaine de formes, et la carte des gabarits n'en
// connaît qu'une.
func normalizeLocale(s string) string {
	if s == "" {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '-' || s[i] == '_' {
			s = s[:i]
			break
		}
	}
	if len(s) > 8 {
		return ""
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c < 'a' || c > 'z' {
			return ""
		}
		out = append(out, c)
	}
	return string(out)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Signal réveille les appareils d'une personne avec des DONNÉES, sans
// notification.
//
// Le SIGNAL d'un appel de course : quand le socket du chauffeur est mort —
// jeton périmé, réseau coupé, service tué, veille profonde — la trame
// `call` n'arrive pas, et il ne sait même pas qu'il a raté quelque chose.
// Ce message-ci passe par FCM, réveille l'application, qui reconnecte son
// socket et traite la trame comme si elle en venait. Rien ne s'affiche par
// le système : c'est l'application qui sonne et ouvre son écran plein.
//
// Pas de gabarit, pas de boîte de réception, pas de préférence : ce n'est
// pas une notification, c'est un signal d'infrastructure. Un chauffeur qui
// aurait coupé « les notifications » doit quand même recevoir ses appels.
func (s *Service) Signal(ctx context.Context, userID string, data map[string]string, ttl time.Duration) (int, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return 0, apperr.Validation("invalid user id").WithCause(err)
	}
	if s.pusher == nil {
		return 0, nil
	}
	devices, err := s.repo.ActiveDevices(ctx, uid)
	if err != nil {
		return 0, apperr.Internal(err)
	}
	if len(devices) == 0 {
		return 0, nil
	}
	msgs := make([]PushMessage, 0, len(devices))
	for _, d := range devices {
		msgs = append(msgs, PushMessage{Token: d.Token, Data: data, DataOnly: true, TTL: ttl})
	}
	results, err := s.pusher.Push(ctx, msgs)
	if err != nil {
		return 0, apperr.New("push_unavailable", "the push service did not answer", 502).WithCause(err)
	}
	sent := 0
	for _, r := range results {
		switch {
		case r.Unregistered:
			if err := s.repo.DisableDevice(ctx, r.Token, "unregistered"); err != nil {
				slog.WarnContext(ctx, "notify: device not disabled", "error", err)
			}
		case r.Err != nil:
			slog.WarnContext(ctx, "notify: signal rejected", "error", r.Err)
		default:
			sent++
		}
	}
	return sent, nil
}
