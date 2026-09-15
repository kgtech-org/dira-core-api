package notify

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Ce fichier porte les CAMPAGNES : un message écrit par l'exploitation et
// envoyé à une POPULATION — tous les clients, tous les chauffeurs, tous les
// marchands — plutôt qu'à une personne à propos d'une commande.
//
// Deux natures, et ce n'est pas la même promesse faite au destinataire :
//
//   - une ALERTE dit quelque chose du service — une panne, une zone
//     inaccessible, une fermeture — et NE SE COUPE PAS : un chauffeur qui a
//     coupé les promotions doit quand même apprendre que le réseau est à
//     terre ;
//   - une CAMPAGNE fait de la promotion, et respecte le réglage
//     « promotions » de chacun. Une promotion reçue par quelqu'un qui les a
//     refusées apprend à couper toutes les notifications.
//
// L'envoi est PROGRAMMABLE, et il est MENÉ en arrière-plan : à des milliers
// de destinataires, une requête HTTP qui attendrait la fin expirerait, et
// l'exploitant ne saurait pas si le message est parti. La campagne porte sa
// progression ; on la relit.

const CollectionCampaigns = "campaigns"

// Natures d'une campagne.
const (
	CampaignAlert     = "alert"
	CampaignPromotion = "campaign"
)

// CategoryAlerts range les alertes de service : une catégorie qui ne se
// coupe pas, comme l'appel de course.
const CategoryAlerts = "alerts"

// États d'une campagne.
const (
	CampaignScheduled = "scheduled" // attend son heure (ou tout de suite)
	CampaignSending   = "sending"   // en cours d'envoi
	CampaignSent      = "sent"      // terminée
	CampaignFailed    = "failed"    // interrompue : voir `error`
	CampaignCancelled = "cancelled" // annulée avant le départ
)

// Rôles qu'une campagne peut viser. Pas l'administration : on ne se fait pas
// de campagne à soi-même.
var campaignRoles = map[string]bool{"client": true, "driver": true, "merchant": true}

// Audience dit À QUI part la campagne.
type Audience struct {
	Roles []string `bson:"roles" json:"roles"`
}

// Campaign est un envoi à une population, persisté avec sa progression.
type Campaign struct {
	ID   primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Kind string             `bson:"kind" json:"kind"`
	// Locales : code de langue → texte. Le français est obligatoire, c'est
	// le repli ; chaque destinataire reçoit la langue de son appareil.
	Locales  map[string]Text `bson:"locales" json:"locales"`
	Audience Audience        `bson:"audience" json:"audience"`
	// Country borne la population : « tous les clients » veut dire ceux du
	// pays que la console regardait en publiant. Posé à la création, lu par
	// l'envoi — qui tourne en tâche de fond, sans requête pour le lui dire.
	Country string `bson:"country,omitempty" json:"country,omitempty"`
	// Data voyage avec la notification jusqu'à l'application : `type`
	// (`alert` | `campaign`), `campaign_id`, et ce que l'exploitant a
	// ajouté — une adresse à ouvrir (`url`), un écran (`screen`).
	Data map[string]string `bson:"data,omitempty" json:"data,omitempty"`
	// Note est pour l'exploitation : pourquoi cette campagne. Jamais envoyée.
	Note string `bson:"note,omitempty" json:"note,omitempty"`

	Status string `bson:"status" json:"status"`
	// SendAt est l'heure demandée ; passée à la création = tout de suite.
	SendAt    time.Time          `bson:"send_at" json:"send_at"`
	CreatedBy primitive.ObjectID `bson:"created_by" json:"created_by"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`

	// La PROGRESSION. Recipients : comptes visés ; Muted : ceux qui ont
	// coupé la catégorie (campagnes seulement) ; Archived : entrées écrites
	// au centre de notifications ; Devices : téléphones joignables ; Pushed
	// et Failed : ce que le transport a dit.
	Recipients int        `bson:"recipients" json:"recipients"`
	Muted      int        `bson:"muted" json:"muted"`
	Archived   int        `bson:"archived" json:"archived"`
	Devices    int        `bson:"devices" json:"devices"`
	Pushed     int        `bson:"pushed" json:"pushed"`
	Failed     int        `bson:"failed" json:"failed"`
	StartedAt  *time.Time `bson:"started_at,omitempty" json:"started_at,omitempty"`
	FinishedAt *time.Time `bson:"finished_at,omitempty" json:"finished_at,omitempty"`
	Error      string     `bson:"error,omitempty" json:"error,omitempty"`
}

// CreateCampaignRequest est ce que la console envoie.
type CreateCampaignRequest struct {
	Kind    string            `json:"kind" validate:"required,oneof=alert campaign"`
	Locales map[string]Text   `json:"locales" validate:"required"`
	Roles   []string          `json:"roles" validate:"required,min=1,max=3,dive,oneof=client driver merchant"`
	Data    map[string]string `json:"data" validate:"omitempty,max=8,dive,keys,max=40,endkeys,max=500"`
	Note    string            `json:"note" validate:"omitempty,max=300"`
	// SendAt absent ou passé = tout de suite.
	SendAt *time.Time `json:"send_at"`
}

// Audiences résout les comptes d'une population, page par page.
//
// Déclaré côté consommateur : le module de notification ne connaît pas les
// comptes, il connaît « les identifiants des comptes actifs de ces rôles,
// après celui-ci ». Implémenté par le module user au câblage.
type Audiences interface {
	AudienceIDs(ctx context.Context, roles []string, after primitive.ObjectID, limit int) ([]primitive.ObjectID, error)
}

// SetAudiences branche la résolution des populations (câblage).
func (s *Service) SetAudiences(a Audiences) { s.audiences = a }

var (
	errCampaignNotFound = apperr.NotFound("campaign_not_found", "campaign not found")
	errCampaignStarted  = apperr.Conflict("campaign_started", "this campaign has already started")
	errNoAudiences      = apperr.New("campaigns_unavailable", "campaigns are not available: no audience resolver", 503)
)

// CreateCampaign enregistre une campagne, et la lance si son heure est venue.
func (s *Service) CreateCampaign(ctx context.Context, adminID string, req CreateCampaignRequest) (*Campaign, error) {
	if s.audiences == nil {
		return nil, errNoAudiences
	}
	by, err := primitive.ObjectIDFromHex(adminID)
	if err != nil {
		return nil, apperr.Validation("invalid admin id").WithCause(err)
	}
	locales, err := cleanLocales(req.Locales)
	if err != nil {
		return nil, err
	}
	roles := uniqueRoles(req.Roles)
	if len(roles) == 0 {
		return nil, apperr.Validation("roles must name client, driver or merchant").
			WithMeta(map[string]any{"fields": []string{"roles"}})
	}
	now := time.Now().UTC()
	sendAt := now
	if req.SendAt != nil && req.SendAt.After(now) {
		sendAt = req.SendAt.UTC()
	}
	data := map[string]string{"type": req.Kind}
	for k, v := range req.Data {
		if k == "type" || k == "campaign_id" {
			continue // ceux-là sont à nous
		}
		data[k] = v
	}
	c := &Campaign{
		Kind: req.Kind, Locales: locales, Audience: Audience{Roles: roles},
		Country: country.FromContext(ctx),
		Data:    data, Note: strings.TrimSpace(req.Note),
		Status: CampaignScheduled, SendAt: sendAt,
		CreatedBy: by, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.InsertCampaign(ctx, c); err != nil {
		return nil, err
	}
	c.Data["campaign_id"] = c.ID.Hex()
	if err := s.repo.SetCampaignData(ctx, c.ID, c.Data); err != nil {
		slog.WarnContext(ctx, "notify: campaign data not stored", "campaign_id", c.ID.Hex(), "error", err)
	}
	if !sendAt.After(now) && s.launch(c) {
		c.Status = CampaignSending
	}
	return c, nil
}

// cleanLocales garde les langues connues, exige le français, et refuse un
// texte vide : une notification sans titre ni corps n'affiche rien.
func cleanLocales(in map[string]Text) (map[string]Text, error) {
	out := map[string]Text{}
	for loc, t := range in {
		loc = normalizeLocale(loc)
		if loc != LocaleFR && loc != LocaleEN {
			continue
		}
		t.Title, t.Body = strings.TrimSpace(t.Title), strings.TrimSpace(t.Body)
		if t.Title == "" && t.Body == "" {
			continue
		}
		if len(t.Title) > MaxTitle*2 || len(t.Body) > MaxBody*2 {
			return nil, apperr.Validation("text too long").WithMeta(map[string]any{"locale": loc})
		}
		out[loc] = t
	}
	if _, ok := out[LocaleFR]; !ok {
		return nil, apperr.Validation("the French text is required: it is the fallback").
			WithMeta(map[string]any{"fields": []string{"locales.fr"}})
	}
	return out, nil
}

func uniqueRoles(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range in {
		if campaignRoles[r] && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// ListCampaigns rend les campagnes, de la plus récente.
func (s *Service) ListCampaigns(ctx context.Context, limit int) ([]Campaign, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.repo.ListCampaigns(ctx, limit)
}

// GetCampaign rend une campagne et sa progression.
func (s *Service) GetCampaign(ctx context.Context, id string) (*Campaign, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errCampaignNotFound
	}
	c, err := s.repo.FindCampaign(ctx, oid)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errCampaignNotFound
	}
	return c, nil
}

// CancelCampaign retire une campagne qui n'est pas encore partie.
//
// Une campagne EN COURS ne s'annule pas : la moitié des téléphones ont déjà
// sonné, et rappeler un message envoyé n'existe pas.
func (s *Service) CancelCampaign(ctx context.Context, id string) (*Campaign, error) {
	c, err := s.GetCampaign(ctx, id)
	if err != nil {
		return nil, err
	}
	ok, err := s.repo.CampaignTransition(ctx, c.ID, CampaignScheduled, CampaignCancelled)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errCampaignStarted
	}
	c.Status = CampaignCancelled
	return c, nil
}

// SendTest envoie le texte d'une campagne AU SEUL APPELANT, sans rien
// enregistrer : c'est ce qui permet de voir la notification sur son propre
// téléphone avant de l'envoyer à des milliers d'autres.
func (s *Service) SendTest(ctx context.Context, adminID string, req CreateCampaignRequest) (int, error) {
	uid, err := primitive.ObjectIDFromHex(adminID)
	if err != nil {
		return 0, apperr.Validation("invalid admin id").WithCause(err)
	}
	locales, err := cleanLocales(req.Locales)
	if err != nil {
		return 0, err
	}
	devices, err := s.repo.ActiveDevices(ctx, uid)
	if err != nil {
		return 0, err
	}
	if s.pusher == nil || len(devices) == 0 {
		return 0, nil
	}
	data := map[string]string{"type": req.Kind, "test": "1"}
	for k, v := range req.Data {
		data[k] = v
	}
	tmpl := Template{Locales: locales}
	msgs := make([]PushMessage, 0, len(devices))
	for _, d := range devices {
		text := pick(tmpl, d.Locale)
		msgs = append(msgs, PushMessage{Token: d.Token, Title: Truncate(text.Title, MaxTitle), Body: Truncate(text.Body, MaxBody), Data: data})
	}
	results, err := s.pusher.Push(ctx, msgs)
	if err != nil {
		return 0, apperr.New("push_failed", "the push transport failed", 502).WithCause(err)
	}
	sent := 0
	for _, r := range results {
		if r.Err == nil && !r.Unregistered {
			sent++
		}
	}
	return sent, nil
}

// RunDueCampaigns lance les campagnes programmées dont l'heure est passée.
// Appelé par un tic régulier (câblage) ; sûr à répéter, sûr sur plusieurs
// instances : c'est la transition `scheduled → sending` qui réserve.
func (s *Service) RunDueCampaigns(ctx context.Context) int {
	due, err := s.repo.DueCampaigns(ctx, time.Now().UTC())
	if err != nil {
		slog.WarnContext(ctx, "notify: due campaigns unreadable", "error", err)
		return 0
	}
	n := 0
	for i := range due {
		c := due[i]
		if s.launch(&c) {
			n++
		}
	}
	return n
}

// launch réserve la campagne (une instance, une fois) et l'envoie en
// arrière-plan. Rend faux si une autre instance l'a déjà prise.
func (s *Service) launch(c *Campaign) bool {
	ctx := context.Background()
	ok, err := s.repo.CampaignTransition(ctx, c.ID, CampaignScheduled, CampaignSending)
	if err != nil || !ok {
		return false
	}
	go s.send(ctx, c)
	return true
}

// Tailles des lots. Les comptes se lisent par pages ; les envois partent par
// paquets de la taille que FCM accepte d'un coup.
const (
	audiencePage = 500
	pushBatch    = 500
)

// send mène la campagne jusqu'au bout et écrit sa progression.
//
// Chaque destinataire reçoit : une entrée au centre de notifications (dans
// la langue de son compte) et une notification par téléphone joignable (dans
// la langue de l'appareil). Une campagne respecte le réglage « promotions »
// de chacun ; une alerte non.
func (s *Service) send(ctx context.Context, c *Campaign) {
	started := time.Now().UTC()
	c.StartedAt = &started
	// L'envoi n'a pas de requête : c'est la campagne qui dit son pays, et
	// le dépôt des comptes borne l'audience comme il le ferait pour une
	// liste de la console. Sans pays (campagne d'avant la couche pays),
	// l'audience est celle de tous les pays — ce qu'elle était alors.
	if c.Country != "" {
		ctx = country.WithCountry(ctx, c.Country, country.SourceClaims)
	}
	category := CategoryPromotions
	if c.Kind == CampaignAlert {
		category = CategoryAlerts
	}
	tmpl := Template{Locales: c.Locales}
	key := "campaign:" + c.ID.Hex()

	var pending []PushMessage
	flush := func() {
		if len(pending) == 0 || s.pusher == nil {
			pending = nil
			return
		}
		results, err := s.pusher.Push(ctx, pending)
		if err != nil {
			slog.WarnContext(ctx, "notify: campaign push batch failed", "campaign_id", c.ID.Hex(), "error", err)
			c.Failed += len(pending)
			pending = nil
			return
		}
		for _, r := range results {
			switch {
			case r.Unregistered:
				c.Failed++
				_ = s.repo.DisableDevice(ctx, r.Token, "unregistered")
			case r.Err != nil:
				c.Failed++
			default:
				c.Pushed++
			}
		}
		pending = nil
	}

	after := primitive.NilObjectID
	for {
		ids, err := s.audiences.AudienceIDs(ctx, c.Audience.Roles, after, audiencePage)
		if err != nil {
			c.Error = "audience: " + err.Error()
			break
		}
		if len(ids) == 0 {
			break
		}
		for _, uid := range ids {
			c.Recipients++
			accountLocale := ""
			if s.users != nil {
				var allowed bool
				accountLocale, allowed = s.users.NotificationPrefs(ctx, uid.Hex(), category)
				if !allowed && Muteable(category) {
					c.Muted++
					continue
				}
			}
			archived := pick(tmpl, accountLocale)
			if err := s.repo.InsertInbox(ctx, &Inbox{
				UserID: uid, Key: key, Category: category,
				Title: Truncate(archived.Title, MaxTitle), Body: Truncate(archived.Body, MaxBody),
				Data: c.Data, CreatedAt: time.Now().UTC(),
			}); err == nil {
				c.Archived++
			}
			devices, err := s.repo.ActiveDevices(ctx, uid)
			if err != nil {
				continue
			}
			for _, d := range devices {
				c.Devices++
				text := pick(tmpl, firstNonEmpty(d.Locale, normalizeLocale(accountLocale)))
				pending = append(pending, PushMessage{
					Token: d.Token, Title: Truncate(text.Title, MaxTitle), Body: Truncate(text.Body, MaxBody), Data: c.Data,
				})
				if len(pending) >= pushBatch {
					flush()
				}
			}
		}
		after = ids[len(ids)-1]
		// La progression s'écrit page par page : une campagne de dix mille
		// personnes se suit depuis la console pendant qu'elle part.
		_ = s.repo.SaveCampaignProgress(ctx, c)
		if len(ids) < audiencePage {
			break
		}
	}
	flush()
	finished := time.Now().UTC()
	c.FinishedAt = &finished
	c.Status = CampaignSent
	if c.Error != "" {
		c.Status = CampaignFailed
	}
	if err := s.repo.SaveCampaignProgress(ctx, c); err != nil {
		slog.ErrorContext(ctx, "notify: campaign result not stored", "campaign_id", c.ID.Hex(), "error", err)
	}
	slog.InfoContext(ctx, "notify: campaign done", "campaign_id", c.ID.Hex(), "kind", c.Kind,
		"recipients", c.Recipients, "muted", c.Muted, "devices", c.Devices, "pushed", c.Pushed, "failed", c.Failed, "status", c.Status)
}
