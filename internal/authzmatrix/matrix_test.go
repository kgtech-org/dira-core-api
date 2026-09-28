// LA MATRICE D'AUTORISATION — route × public, éprouvée sur une plateforme VIVANTE.
//
// ⚠️ C'EST LA CLASSE DE DÉFAUT QUI COÛTE LE PLUS CHER, ET AUCUN SCANNER NE LA
// TROUVE. Un `govulncheck` dit ce que le code embarque ; un `gitleaks` dit ce
// qu'on a laissé traîner. Ni l'un ni l'autre ne dit qu'une route
// d'administration a été montée hors du groupe authentifié — et ce jour-là, la
// liste des comptes d'un pays devient lisible par n'importe qui.
//
// ⚠️ ET UN TEST UNITAIRE NE PEUT PAS LE DIRE. Le routeur du socle se construit
// avec Mongo, Redis, la messagerie et l'annuaire du staff : le monter dans un
// test demanderait de simuler la moitié de la plateforme, et ce qu'on
// vérifierait alors serait la simulation. Cette vérification-là ne vaut que
// contre le vrai service, avec de vrais jetons.
//
// D'où la forme : le test est INERTE sans `DIRA_MATRIX_BASE_URL`, donc la CI
// reste verte et rapide ; on le lance à la main, ou après un déploiement.
//
//	DIRA_MATRIX_BASE_URL=https://api-staging.dira.llc/api/v1 \
//	DIRA_MATRIX_COUNTRY=TG \
//	go test ./internal/authzmatrix/ -run TestAuthorizationMatrix -v
//
// ⚠️ IL NE VOIT QUE CE QUE LE CONTRAT DÉCLARE, et c'est la limite à connaître :
// une route montée dans le code mais absente d'`openapi.yaml` échappe à la
// matrice. `GET /stores/{id}/ratings` en est un exemple trouvé au premier
// passage — publique, réelle, et non documentée. La matrice ne remplace donc pas
// la tenue du contrat : elle la récompense.
//
// ⚠️ IL NE LIT QUE DES `GET`. Parcourir chaque route avec cinq jetons en
// envoyant des `POST` écrirait dans la base du pays qu'on interroge — un test
// de sécurité qui abîme les données qu'il protège serait coupé le lendemain, et
// à juste titre. Les fuites de lecture sont de toute façon celles qui comptent.
package authzmatrix

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// publicPrefixes déclare ce qui est OUVERT sans jeton, et rien d'autre.
//
// ⚠️ C'EST UNE DÉCLARATION, PAS UNE CONSTATATION. Le test refuse tout `2xx`
// anonyme hors de cette liste : ajouter une entrée ici est donc un geste qu'on
// fait EXPRÈS, qui se relit dans une revue, et qui demande de dire pourquoi.
// Sans cette liste, le test aurait constaté ce qui existe — et béni la fuite du
// jour où quelqu'un ouvre une route par accident.
var publicPrefixes = []string{
	"/countries",          // la liste des pays, avant toute connexion
	"/map-markers",        // les pictogrammes de la carte, les mêmes pour tous
	"/payments/providers", // les moyens de paiement offerts dans ce pays
	"/health",             // la sonde
	"/auth/",              // se connecter, s'inscrire, rafraîchir
	"/password/",          // mot de passe oublié
	"/files/",             // un média servi par son adresse
	"/openapi",            // le contrat lui-même
	"/docs",               // sa présentation
	// Les NOTES sont publiques à dessein : montées hors de tout groupe
	// authentifié (`internal/rating/handler.go`), comme les avis d'une vitrine —
	// on les lit avant de créer un compte.
	//
	// ⚠️ RÉSERVE À TRANCHER, PAS À TAIRE : `/agents/{id}/ratings` publie les notes
	// d'une PERSONNE. Quiconque tient un identifiant de livreur lit son historique,
	// sans compte et sans trace. Pour un plat ou une enseigne c'est une vitrine ;
	// pour quelqu'un qui travaille, c'est un dossier. La question est produit, pas
	// technique — ce test la pose, il ne la résout pas.
	"/stores/",
	"/agents/",
	"/dishes/",
}

func TestAuthorizationMatrix(t *testing.T) {
	base := strings.TrimRight(os.Getenv("DIRA_MATRIX_BASE_URL"), "/")
	if base == "" {
		t.Skip("DIRA_MATRIX_BASE_URL absent : la matrice ne s'éprouve que contre une plateforme vivante")
	}
	country := os.Getenv("DIRA_MATRIX_COUNTRY")
	if country == "" {
		country = "TG"
	}

	routes := getRoutes(t)
	t.Logf("%d routes GET déclarées dans le contrat", len(routes))

	audiences := signIn(t, base, country)
	t.Logf("%d publics ouverts : %s", len(audiences), strings.Join(names(audiences), ", "))

	var leaks []string
	for _, route := range routes {
		path := fill(route)
		for _, a := range audiences {
			status := probe(t, base, path, country, a.token)
			if bad := judge(route, a, status); bad != "" {
				leaks = append(leaks, fmt.Sprintf("%-42s %-9s → %d  %s", route, a.name, status, bad))
			}
		}
	}
	sort.Strings(leaks)
	for _, l := range leaks {
		t.Errorf("FUITE  %s", l)
	}
	if len(leaks) == 0 {
		t.Logf("aucune fuite : %d routes × %d publics", len(routes), len(audiences))
	}
}

// judge dit ce qui est INTERDIT, et se tait sur le reste.
//
// ⚠️ IL NE JUGE QUE CE DONT ON EST SÛR. « Ce chauffeur devrait-il voir cette
// course ? » demande de connaître le métier, et un test qui le devinerait
// rendrait des verdicts faux qu'on finirait par ignorer en bloc. Trois règles
// seulement, mais indiscutables — et c'est ce qui les rend utiles.
func judge(route string, a audience, status int) string {
	ok := status >= 200 && status < 300
	switch {
	// ⚠️ UN `429` N'EST PAS UN VERDICT. La cadence refuse la requête AVANT que
	// l'autorisation ait été consultée : compté comme « bien protégé », il
	// transformerait la matrice en test qui passe toujours — et d'autant mieux
	// qu'elle sonde vite. C'est arrivé au premier essai : deux cents sondages
	// depuis une seule adresse, et la fin du parcours n'a rien éprouvé.
	case status == http.StatusTooManyRequests:
		return "la cadence a refusé la requête : cette case n'a PAS été éprouvée"
	// ⚠️ LES ROUTES INTERNES NE SORTENT PAS. Elles portent le jeton de service
	// et n'ont aucune borne de pays : atteignables du dehors, elles rendent
	// tout, pour tous les pays.
	case strings.Contains(route, "/internal/") && ok:
		return "une route interne répond depuis l'extérieur"
	// L'administration n'est pas pour les autres publics.
	case strings.HasPrefix(route, "/admin") && a.name != "console" && ok:
		return "l'administration répond à un public qui n'en est pas"
	// Et rien d'autre ne s'ouvre sans jeton.
	case a.name == "anonyme" && ok && !isPublic(route):
		return "une route non déclarée publique répond sans jeton"
	}
	return ""
}

func isPublic(route string) bool {
	for _, p := range publicPrefixes {
		if strings.HasPrefix(route, p) {
			return true
		}
	}
	return false
}

type audience struct {
	name  string
	token string
}

func names(as []audience) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.name)
	}
	return out
}

// signIn ouvre une session par public. Un compte absent du jeu de données est
// IGNORÉ plutôt que fatal : la matrice doit pouvoir tourner sur un pays qui n'a
// pas encore de marchand, sinon elle ne tournera jamais.
func signIn(t *testing.T, base, country string) []audience {
	out := []audience{{name: "anonyme"}}
	type cred struct{ name, phone, email, app string }
	for _, c := range []cred{
		{name: "console", email: os.Getenv("DIRA_MATRIX_ADMIN_EMAIL"), app: "console"},
		{name: "client", phone: os.Getenv("DIRA_MATRIX_CLIENT_PHONE"), app: "client"},
		{name: "chauffeur", phone: os.Getenv("DIRA_MATRIX_DRIVER_PHONE"), app: "driver"},
		{name: "livreur", phone: os.Getenv("DIRA_MATRIX_COURIER_PHONE"), app: "courier"},
	} {
		if c.phone == "" && c.email == "" {
			t.Logf("public %q sans identifiants : ignoré", c.name)
			continue
		}
		body := map[string]string{"password": os.Getenv("DIRA_MATRIX_PASSWORD"), "app": c.app}
		if c.email != "" {
			body["email"] = c.email
			body["password"] = os.Getenv("DIRA_MATRIX_ADMIN_PASSWORD")
		} else {
			body["phone"] = c.phone
		}
		token := login(t, base, country, body)
		if token == "" {
			t.Logf("public %q : connexion refusée, ignoré", c.name)
			continue
		}
		out = append(out, audience{name: c.name, token: token})
	}
	return out
}

func login(t *testing.T, base, country string, body map[string]string) string {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, base+"/auth/login", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Dira-Country", country)
	resp, err := client().Do(req)
	if err != nil {
		t.Fatalf("connexion injoignable : %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.AccessToken
}

// pace espace les sondages pour rester sous la limite de cadence.
//
// ⚠️ SANS CELA LA MATRICE SE FAIT REFUSER PAR SA PROPRE PROTECTION. Tout le
// trafic anonyme d'une même adresse partage un compartiment (c'est voulu, voir
// `middleware.ClientIP`) : deux cents sondages en quelques secondes l'épuisent,
// et les dernières routes ne sont jamais éprouvées.
// ⚠️ 600 ms, ET C'EST MESURÉ, PAS CHOISI AU HASARD. La limite servie est de 120
// requêtes par minute et par adresse ; un parcours complet fait environ deux
// cents sondages. À 250 ms, tout tenait dans une minute et le dernier tiers se
// faisait refuser — le test annonçait alors des cases non éprouvées, ce qui est
// honnête mais inutile. À 600 ms le parcours s'étale sur deux minutes, soit une
// centaine de sondages par minute.
//
// ⚠️ ET SI LE PARCOURS S'ALLONGE, CETTE VALEUR DEVRA SUIVRE. Le jour où le
// contrat déclare cent routes GET, la matrice se refusera toute seule — elle le
// DIRA (`429` n'est pas un verdict), mais il faudra y revenir.
const pace = 600 * time.Millisecond

func probe(t *testing.T, base, path, country, token string) int {
	time.Sleep(pace)
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Dira-Country", country)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client().Do(req)
	if err != nil {
		t.Fatalf("%s injoignable : %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func client() *http.Client { return &http.Client{Timeout: 20 * time.Second} }

var param = regexp.MustCompile(`\{[^}]+\}`)

// fill remplace un paramètre de chemin par une valeur qui n'existe pas.
//
// ⚠️ UNE VALEUR INEXISTANTE SUFFIT, et c'est même ce qu'on veut : la question
// posée est « cette route répond-elle à ce public ? », pas « que contient cet
// objet ? ». Un `404` prouve qu'on a passé l'autorisation ; c'est le `200` qui
// accuse. Passer un identifiant RÉEL ferait au contraire courir le risque de
// modifier ou de révéler quelque chose.
func fill(route string) string {
	return param.ReplaceAllString(route, "000000000000000000000000")
}

func getRoutes(t *testing.T) []string {
	raw, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatalf("contrat introuvable : %v", err)
	}
	// ⚠️ LECTURE À LA MAIN, SANS BIBLIOTHÈQUE YAML. Le socle n'en dépend pas, et
	// l'ajouter pour un test ferait entrer une dépendance dans le binaire de
	// production. On ne cherche ici que les clés de premier niveau de `paths:`
	// et la présence d'un `get:` — ce que l'indentation suffit à dire.
	var out []string
	var current string
	inPaths := false
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "paths:"):
			inPaths = true
		case inPaths && len(line) > 0 && line[0] != ' ':
			inPaths = false
		case inPaths && strings.HasPrefix(line, "  /"):
			current = strings.TrimSuffix(strings.TrimSpace(line), ":")
		case inPaths && current != "" && strings.HasPrefix(line, "    get:"):
			out = append(out, current)
		}
	}
	if len(out) == 0 {
		t.Fatal("aucune route GET lue dans le contrat : la lecture est cassée, pas le contrat")
	}
	sort.Strings(out)
	return out
}
