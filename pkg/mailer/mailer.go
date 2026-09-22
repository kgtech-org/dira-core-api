// Package mailer envoie du courrier électronique — la seule porte SMTP de la
// plateforme.
//
// ⚠️ IL EST AU SOCLE, comme `fcm`. Notifier quelqu'un est une capacité de
// plateforme, pas une particularité d'un métier : les rapports périodiques
// l'utilisent aujourd'hui, une réinitialisation de mot de passe l'utilisera
// demain, et deux clients SMTP auraient donné deux expéditeurs, deux
// configurations et deux façons d'échouer.
//
// ⚠️ IL NE REND JAMAIS UN SUCCÈS QU'IL N'A PAS OBTENU. Un client non
// configuré refuse À VOIX HAUTE (`ErrNotConfigured`) au lieu de faire comme
// si le message était parti : un rapport qu'on croit envoyé est pire qu'un
// rapport manquant — personne ne va le chercher.
package mailer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// ErrNotConfigured : aucun serveur SMTP n'est réglé. L'appelant doit le DIRE
// à qui attend le message, pas l'avaler.
var ErrNotConfigured = errors.New("mailer: SMTP is not configured")

// Config est le serveur et le compte d'envoi.
type Config struct {
	Host string
	Port int
	User string
	Pass string
	// From est l'adresse d'expédition, et FromName le nom affiché.
	From     string
	FromName string
	// TLS dit comment la connexion est chiffrée :
	//
	//	implicit  la connexion est chiffrée dès l'ouverture (port 465)
	//	starttls  la connexion s'ouvre en clair puis se chiffre (port 587)
	//
	// ⚠️ PAS DE TROISIÈME VALEUR. « aucun » n'est pas proposé : un mot de
	// passe SMTP envoyé en clair sur Internet est un mot de passe perdu, et
	// une option pour le faire finit toujours par être cochée.
	TLS string
	// Timeout borne un envoi. Un serveur muet ne doit pas retenir le
	// planificateur des rapports.
	Timeout time.Duration
}

// TLS modes.
const (
	TLSImplicit = "implicit"
	TLSStartTLS = "starttls"
)

// Message est un courrier.
type Message struct {
	To      []string
	Subject string
	// Text est la version lisible sans mise en forme. TOUJOURS remplie :
	// une part HTML seule tombe dans les indésirables chez la moitié des
	// fournisseurs, et certains lecteurs n'affichent que celle-ci.
	Text string
	HTML string
	// ReplyTo, quand la réponse ne doit pas aller à l'expéditeur.
	ReplyTo string
}

// Mailer est ce que les appelants déclarent avoir besoin de connaître.
type Mailer interface {
	Send(ctx context.Context, m Message) error
	// Configured dit si un envoi a une chance d'aboutir. Permet à une
	// console d'afficher « courrier non configuré » plutôt que de proposer
	// un bouton qui échouera.
	Configured() bool
	// From est l'adresse d'expédition, pour l'afficher.
	From() string
}

// SMTP est l'implémentation.
type SMTP struct {
	cfg Config
}

var _ Mailer = (*SMTP)(nil)

// New construit le client. Il accepte une configuration INCOMPLÈTE sans
// broncher : c'est le cas normal en développement, et `Configured()` le dit.
func New(cfg Config) *SMTP {
	if cfg.Port == 0 {
		cfg.Port = 465
	}
	if cfg.TLS != TLSStartTLS {
		cfg.TLS = TLSImplicit
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 20 * time.Second
	}
	if cfg.From == "" {
		cfg.From = cfg.User
	}
	return &SMTP{cfg: cfg}
}

func (s *SMTP) Configured() bool {
	return s != nil && s.cfg.Host != "" && s.cfg.From != ""
}

func (s *SMTP) From() string {
	if s == nil {
		return ""
	}
	return s.cfg.From
}

// Send remet le message au serveur.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	if !s.Configured() {
		return ErrNotConfigured
	}
	to := cleanAddresses(m.To)
	if len(to) == 0 {
		// Refusé plutôt qu'ignoré : un envoi sans destinataire est une
		// erreur de configuration, et la taire la ferait durer des mois.
		return fmt.Errorf("mailer: no recipient")
	}
	raw, err := s.Build(m, to, time.Now())
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	client, err := s.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if s.cfg.User != "" {
		auth := smtp.PlainAuth("", s.cfg.User, s.cfg.Pass, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("mailer: authenticate: %w", err)
		}
	}
	if err := client.Mail(s.cfg.From); err != nil {
		return fmt.Errorf("mailer: sender refused: %w", err)
	}
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("mailer: recipient %s refused: %w", addr, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("mailer: data: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		_ = w.Close()
		return fmt.Errorf("mailer: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mailer: close body: %w", err)
	}
	return client.Quit()
}

func (s *SMTP) dial(ctx context.Context) (*smtp.Client, error) {
	addr := net.JoinHostPort(s.cfg.Host, fmt.Sprint(s.cfg.Port))
	d := &net.Dialer{}
	switch s.cfg.TLS {
	case TLSStartTLS:
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("mailer: dial %s: %w", addr, err)
		}
		client, err := smtp.NewClient(conn, s.cfg.Host)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("mailer: smtp: %w", err)
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			_ = client.Close()
			// ⚠️ On ABANDONNE plutôt que de continuer en clair. Un repli
			// silencieux enverrait le mot de passe du compte d'envoi en
			// clair sur Internet.
			return nil, fmt.Errorf("mailer: starttls: %w", err)
		}
		return client, nil
	default:
		conn, err := (&tls.Dialer{
			NetDialer: d,
			Config:    &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12},
		}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("mailer: dial %s: %w", addr, err)
		}
		client, err := smtp.NewClient(conn, s.cfg.Host)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("mailer: smtp: %w", err)
		}
		return client, nil
	}
}

// Build rend le message brut (RFC 5322). Exporté pour être ÉPROUVÉ : c'est
// la partie où l'on se trompe — un accent mal encodé, une frontière
// multipartie absente, une ligne d'en-tête qui manque — et elle ne demande
// aucun serveur pour être vérifiée.
func (s *SMTP) Build(m Message, to []string, now time.Time) ([]byte, error) {
	if strings.ContainsAny(m.Subject, "\r\n") {
		// Une injection d'en-tête par le sujet ajouterait des
		// destinataires cachés. Refusé, jamais nettoyé en silence.
		return nil, fmt.Errorf("mailer: subject must not contain a line break")
	}
	from := (&mail.Address{Name: s.cfg.FromName, Address: s.cfg.From}).String()
	boundary := fmt.Sprintf("dira-%d", now.UnixNano())

	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	if m.ReplyTo != "" {
		b.WriteString("Reply-To: " + m.ReplyTo + "\r\n")
	}
	// Le sujet est encodé : « Rapport journalier — Lomé » perdrait ses
	// accents et son tiret cadratin en ASCII brut.
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", m.Subject) + "\r\n")
	b.WriteString("Date: " + now.Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	// `Auto-Submitted` dit aux serveurs et aux répondeurs automatiques que
	// ce courrier est généré : sans lui, une absence du bureau répond au
	// rapport, et la boîte d'envoi part en boucle.
	b.WriteString("Auto-Submitted: auto-generated\r\n")

	text := m.Text
	if text == "" {
		text = "Ce message nécessite un lecteur de courrier capable d'afficher le HTML."
	}
	if m.HTML == "" {
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		b.WriteString(normalize(text))
		return []byte(b.String()), nil
	}

	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
	// L'ordre compte : le lecteur affiche la DERNIÈRE part qu'il sait lire.
	// Le texte d'abord, le HTML ensuite.
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(normalize(text))
	b.WriteString("\r\n--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(normalize(m.HTML))
	b.WriteString("\r\n--" + boundary + "--\r\n")
	return []byte(b.String()), nil
}

// normalize met les fins de ligne au format du courrier et protège le point
// isolé, qui termine un message SMTP.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")
	return strings.ReplaceAll(s, "\r\n.\r\n", "\r\n..\r\n")
}

// cleanAddresses garde les adresses PLAUSIBLES et jette les vides.
//
// ⚠️ Une adresse invalide ne fait pas échouer tout l'envoi : un destinataire
// mal saisi dans une liste de six ne doit pas priver les cinq autres de leur
// rapport. Celui qui manque est visible dans le journal des envois.
func cleanAddresses(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		addr := strings.TrimSpace(raw)
		if addr == "" || seen[strings.ToLower(addr)] {
			continue
		}
		if _, err := mail.ParseAddress(addr); err != nil {
			continue
		}
		seen[strings.ToLower(addr)] = true
		out = append(out, addr)
	}
	return out
}

// Valid dit si une adresse est acceptable. Servie ici pour que la console et
// le serveur appliquent la MÊME règle.
func Valid(addr string) bool {
	_, err := mail.ParseAddress(strings.TrimSpace(addr))
	return err == nil
}
