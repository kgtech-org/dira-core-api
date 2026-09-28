package country

import (
	"context"
	"log/slog"
	"sort"
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
	currency map[string]string // réglages de monnaie, par pays
	// testing : les pays RÉSERVÉS AUX ESSAIS. Lu par tout ce qui compte —
	// supervision, rapports, classements — pour ne pas mêler des courses
	// fabriquées à de vraies.
	testing  map[string]bool
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
	cur := make(map[string]string, len(rows))
	test := make(map[string]bool, len(rows))
	for _, r := range rows {
		m[r.Code] = r.Enabled
		if r.Currency != "" {
			cur[r.Code] = r.Currency
		}
		if r.Testing {
			test[r.Code] = true
		}
	}
	s.mu.Lock()
	s.enabled, s.currency, s.testing, s.loadedAt = m, cur, test, time.Now()
	s.mu.Unlock()
}

// CurrencyOf rend la monnaie EN VIGUEUR d'un pays : le réglage s'il y en a
// un, sinon celle du catalogue.
func (s *Service) CurrencyOf(code string) country.Currency {
	info, ok := country.Lookup(code)
	if !ok {
		return country.Currency{}
	}
	s.mu.RLock()
	override := s.currency[info.Code]
	s.mu.RUnlock()
	if override != "" {
		if c, ok := country.LookupCurrency(override); ok {
			return c
		}
	}
	c, _ := country.LookupCurrency(info.Currency)
	return c
}

// respond assemble la réponse d'un pays avec sa monnaie effective.
func (s *Service) respond(info country.Info, enabled bool) Response {
	cur := s.CurrencyOf(info.Code)
	info.Currency = cur.Code
	return Response{
		Info: info, Enabled: enabled, Default: info.Code == s.defaultCode,
		Testing:      s.Testing(info.Code),
		CurrencyName: cur.Name, CurrencySymbol: cur.Symbol, CurrencyDecimals: cur.Decimals,
	}
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
		out = append(out, s.respond(info, enabled))
	}
	return out, nil
}

// Update règle un pays du catalogue : ouvert ou fermé, et sa monnaie.
func (s *Service) Update(ctx context.Context, code string, req UpdateRequest) (*Response, error) {
	info, ok := country.Lookup(code)
	if !ok {
		return nil, errUnknownCountry
	}
	if req.Enabled == nil && req.Currency == nil && req.Testing == nil {
		return nil, errNothingToUpdate
	}
	if req.Testing != nil && *req.Testing && info.Code == s.defaultCode {
		return nil, errDefaultTesting
	}
	before := map[string]any{"code": info.Code, "enabled": s.Enabled(info.Code), "currency": s.CurrencyOf(info.Code).Code}
	if req.Enabled != nil {
		if info.Code == s.defaultCode && !*req.Enabled {
			return nil, errDefaultCountry
		}
		if err := s.repo.SetEnabled(ctx, info.Code, *req.Enabled); err != nil {
			return nil, apperr.Internal(err)
		}
	}
	if req.Currency != nil {
		// Vide = revenir à la monnaie du catalogue.
		cur := country.NormalizeCurrency(*req.Currency)
		if *req.Currency != "" {
			if _, ok := country.LookupCurrency(cur); !ok {
				return nil, errUnknownCurrency.WithMeta(map[string]any{"fields": []string{"currency"}})
			}
		}
		if err := s.repo.SetCurrency(ctx, info.Code, cur); err != nil {
			return nil, apperr.Internal(err)
		}
	}
	if req.Testing != nil {
		if err := s.repo.SetTesting(ctx, info.Code, *req.Testing); err != nil {
			return nil, apperr.Internal(err)
		}
	}
	s.refresh(ctx)
	out := s.respond(info, s.Enabled(info.Code))
	if s.audit != nil {
		s.audit.Record(ctx, "country.update", "country", info.Code, before,
			map[string]any{"code": info.Code, "enabled": out.Enabled, "currency": out.Currency})
	}
	return &out, nil
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

// Testing dit si ce pays est réservé aux ESSAIS.
//
// ⚠️ AU MIEUX DANS LE SENS QUI PROTÈGE LES CHIFFRES : cache vide ou base
// injoignable répond `false`, donc le pays compte. Se tromper dans ce sens fait
// apparaître des courses d'essai dans un tableau ; se tromper dans l'autre
// ferait DISPARAÎTRE de vraies courses des rapports, et personne ne cherche ce
// qu'il ne voit pas manquer.
func (s *Service) Testing(code string) bool {
	code = country.Normalize(code)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.testing[code]
}

// TestingCodes rend les pays d'essai, triés — ce qu'une verticale exclut de ses
// mesures et de ses rapports.
func (s *Service) TestingCodes() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.testing))
	for code := range s.testing {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}
