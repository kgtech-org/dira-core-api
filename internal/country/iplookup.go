package country

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/kgtech-org/dira-core-api/pkg/country"
)

// HTTPLookup situe une adresse IP par un fournisseur HTTP qui répond en
// JSON — ip-api.com par défaut, le seul qui se passe de clé.
//
// ⚠️ C'est le REPLI, pas la règle : une adresse IP mobile sort parfois par
// la passerelle d'un autre pays, et un VPN dit n'importe quoi. La position
// de l'appareil passe avant ; l'IP ne sert que sans position, ou quand la
// position est hors zone. Le fournisseur se règle par
// `COUNTRY_IP_LOOKUP_URL` (le motif `{ip}` y est remplacé) et
// `COUNTRY_IP_LOOKUP_FIELD` (le champ JSON qui porte le code).
//
// Les réponses sont gardées 24 h dans Redis : le même téléphone demande son
// pays à chaque démarrage, et le fournisseur gratuit tolère 45 requêtes par
// minute.
type HTTPLookup struct {
	URL    string // ex. http://ip-api.com/json/{ip}?fields=countryCode
	Field  string // ex. countryCode
	Client *http.Client
	Cache  *redis.Client
}

// DefaultIPLookupURL et DefaultIPLookupField : ip-api.com, sans clé, usage
// non commercial — à remplacer en production par un fournisseur sous
// contrat (ipinfo, MaxMind) via l'environnement.
const (
	DefaultIPLookupURL   = "http://ip-api.com/json/{ip}?fields=countryCode"
	DefaultIPLookupField = "countryCode"
	ipCacheTTL           = 24 * time.Hour
)

// Country implements IPLookup.
func (l *HTTPLookup) Country(ctx context.Context, ip string) (string, error) {
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil || addr.IsPrivate() || addr.IsLoopback() || addr.IsUnspecified() || addr.IsLinkLocalUnicast() {
		return "", nil // une adresse de réseau local n'est nulle part
	}
	key := "country:ip:" + addr.String()
	if l.Cache != nil {
		if got, err := l.Cache.Get(ctx, key).Result(); err == nil {
			return got, nil
		}
	}
	url := strings.ReplaceAll(l.URL, "{ip}", addr.String())
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ip lookup: %s answered %d", res.Request.URL.Host, res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("ip lookup: not JSON: %w", err)
	}
	code, _ := payload[l.Field].(string)
	code = country.Normalize(code)
	if l.Cache != nil {
		// Même vide : ne pas redemander pendant 24 h une adresse que le
		// fournisseur ne sait pas situer.
		_ = l.Cache.Set(ctx, key, code, ipCacheTTL).Err()
	}
	return code, nil
}

// ClientIP rend l'adresse d'origine d'une requête passée par la passerelle
// (`X-Forwarded-For`, première adresse), sinon celle de la connexion.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
