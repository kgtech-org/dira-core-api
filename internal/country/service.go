package country

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kgtech-org/dira-core-api/pkg/apperr"
	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// Accounts est ce que ce module demande aux comptes : le pays d'une personne,
// et le moyen de le changer. Déclaré ici, côté consommateur.
type Accounts interface {
	CountryOf(ctx context.Context, userID string) (string, error)
	SetCountry(ctx context.Context, userID, code string) error
}

// IPLookup situe une adresse IP dans un pays. Vide si le fournisseur ne
// sait pas — jamais une erreur pour une adresse privée.
type IPLookup interface {
	Country(ctx context.Context, ip string) (string, error)
}

// Auditor journalise l'ouverture et la fermeture d'un pays.
type Auditor interface {
	Record(ctx context.Context, action, resourceType, resourceID string, before, after any)
}

// Service is the country layer of the core.
type Service struct {
	repo        *Repository
	defaultCode string
	accounts    Accounts
	ips         IPLookup
	audit       Auditor

	// Le cache des pays ouverts, lu à CHAQUE requête par le middleware :
	// une lecture Mongo par requête pour une liste qui change une fois par
	// trimestre aurait été le prix d'une précision dont personne n'a besoin.
	mu       sync.RWMutex
	enabled  map[string]bool
	loadedAt time.Time
	cacheTTL time.Duration
}

const cacheTTL = time.Minute

// NewService builds the service. defaultCode is the deployment's default
// country, opened at start if it has never been registered.
func NewService(repo *Repository, defaultCode string) *Service {
	s := &Service{repo: repo, defaultCode: country.Normalize(defaultCode), cacheTTL: cacheTTL}
	if s.defaultCode == "" || !country.Known(s.defaultCode) {
		s.defaultCode = "TG"
	}
	return s
}

func (s *Service) SetAccounts(a Accounts) { s.accounts = a }
func (s *Service) SetIPLookup(l IPLookup) { s.ips = l }
func (s *Service) SetAuditor(a Auditor)   { s.audit = a }

// Start ouvre le pays par défaut et les pays PRÉCHARGÉS (`country.Preloaded`
// : Togo, Sénégal, Guinée) s'ils n'ont jamais été enregistrés, et charge le
// cache. Une erreur ici ne doit pas empêcher le socle de démarrer : le
// middleware retombe sur le catalogue tant que le cache est vide.
func (s *Service) Start(ctx context.Context) {
	for _, code := range append([]string{s.defaultCode}, country.Preloaded...) {
		if err := s.repo.EnsureEnabled(ctx, code); err != nil {
			slog.WarnContext(ctx, "country: preloaded country not registered", "code", code, "error", err)
		}
	}
	s.refresh(ctx)
}

func (s *Service) refresh(ctx context.Context) {
	if s.repo == nil {
		return
	}
	rows, err := s.repo.All(ctx)
	if err != nil {
		slog.WarnContext(ctx, "country: installed list unavailable, keeping the previous one", "error", err)
		return
	}
	m := make(map[string]bool, len(rows))
	for _, r := range rows {
		m[r.Code] = r.Enabled
	}
	s.mu.Lock()
	s.enabled, s.loadedAt = m, time.Now()
	s.mu.Unlock()
}

// Enabled dit si un pays est OUVERT. Implémente `middleware.Installed`.
//
// AU MIEUX : cache vide (base injoignable au démarrage) = le catalogue fait
// foi, pour ne pas fermer la porte à tout le monde sur une panne.
func (s *Service) Enabled(code string) bool {
	code = country.Normalize(code)
	if !country.Known(code) {
		return false
	}
	s.mu.RLock()
	m, loadedAt := s.enabled, s.loadedAt
	s.mu.RUnlock()
	if m == nil {
		return true
	}
	if time.Since(loadedAt) > s.cacheTTL {
		go s.refresh(context.Background())
	}
	return code == s.defaultCode || m[code]
}

// Default rend le pays par défaut du déploiement.
func (s *Service) Default() string { return s.defaultCode }

// List rend le catalogue avec l'état d'installation de chaque pays.
// `onlyEnabled` = ce que le public voit.
func (s *Service) List(ctx context.Context, onlyEnabled bool) ([]Response, error) {
	rows, err := s.repo.All(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	state := make(map[string]bool, len(rows))
	for _, r := range rows {
		state[r.Code] = r.Enabled
	}
	out := make([]Response, 0, len(country.Catalog))
	for _, info := range country.Catalog {
		enabled := state[info.Code] || info.Code == s.defaultCode
		if onlyEnabled && !enabled {
			continue
		}
		out = append(out, Response{Info: info, Enabled: enabled, Default: info.Code == s.defaultCode})
	}
	return out, nil
}

// SetEnabled ouvre ou ferme un pays du catalogue.
func (s *Service) SetEnabled(ctx context.Context, code string, enabled bool) (*Response, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if info.Code == s.defaultCode && !enabled {
		return nil, errDefaultCountry
	}
	before := map[string]any{"code": info.Code, "enabled": s.Enabled(info.Code)}
	if err := s.repo.SetEnabled(ctx, info.Code, enabled); err != nil {
		return nil, apperr.Internal(err)
	}
	s.refresh(ctx)
	if s.audit != nil {
		s.audit.Record(ctx, "country.set_enabled", "country", info.Code, before,
			map[string]any{"code": info.Code, "enabled": enabled})
	}
	return &Response{Info: info, Enabled: enabled, Default: info.Code == s.defaultCode}, nil
}

// Resolve répond à « dans quel pays suis-je ? » et aligne le compte.
//
// Deux signaux, dans l'ordre : la POSITION de l'appareil, si l'application
// l'envoie — c'est la vérité, à cent mètres près — puis l'ADRESSE IP de la
// requête, moins sûre (un opérateur mobile sort parfois par un autre pays)
// mais toujours disponible. Un signal qui désigne un pays NON installé ne
// vaut pas : la personne est hors zone, et on lui garde le pays de son
// compte plutôt que de la mettre dans un pays qui ne sert rien.
//
// ⚠️ Le compte est mis à jour quand le pays retenu est SÛR (position ou IP
// dans un pays installé) et différent du sien. Un voyageur qui ouvre
// l'application depuis Cotonou devient béninois pour Dira — c'est ce qu'il
// veut : commander à Cotonou. Son historique togolais ne bouge pas, il est
// stampé de son pays d'alors.
func (s *Service) Resolve(ctx context.Context, userID, ip string, req ResolveRequest) (*ResolveResponse, error) {
	if (req.Lng == nil) != (req.Lat == nil) {
		return nil, errNoCoordinates
	}
	out := &ResolveResponse{}
	if req.Lng != nil {
		if code, ok := country.Locate(*req.Lng, *req.Lat); ok {
			out.Detected = code
			if s.Enabled(code) {
				out.Country, out.Source, out.Supported = code, ResolvedByGeo, true
			}
		}
	}
	if out.Country == "" && s.ips != nil && ip != "" {
		code, err := s.ips.Country(ctx, ip)
		if err != nil {
			slog.WarnContext(ctx, "country: ip lookup failed", "error", err)
		} else if code != "" {
			if out.Detected == "" {
				out.Detected = code
			}
			if s.Enabled(code) {
				out.Country, out.Source, out.Supported = code, ResolvedByIP, true
			}
		}
	}

	current := ""
	if s.accounts != nil && userID != "" {
		got, err := s.accounts.CountryOf(ctx, userID)
		if err != nil {
			return nil, err
		}
		current = got
	}
	if out.Country == "" {
		if current != "" {
			out.Country, out.Source = current, ResolvedByAccount
		} else {
			out.Country, out.Source = s.defaultCode, ResolvedByDefault
		}
	}
	if s.accounts != nil && userID != "" && out.Country != current {
		if err := s.accounts.SetCountry(ctx, userID, out.Country); err != nil {
			return nil, err
		}
		out.Updated = true
	}
	return out, nil
}
