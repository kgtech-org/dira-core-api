package receipt

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrLinesDoNotAddUp : le détail du prix ne fait pas le total.
//
// ⚠️ C'EST UN REFUS DE RENDRE, PAS UN AVERTISSEMENT, et c'est le choix le plus
// important de ce paquet. Un reçu dont les lignes ne s'additionnent pas est le
// pire document que la plateforme puisse produire : il PROUVE une erreur, il est
// signé de notre nom, et c'est le client qui le découvre — souvent devant son
// comptable. Mieux vaut une erreur 500 qu'un PDF faux : la première est un
// incident qu'on corrige, le second est une réclamation qu'on ne peut pas
// gagner.
var ErrLinesDoNotAddUp = errors.New("receipt: les lignes de détail ne font pas le total")

// Check vérifie ce qu'un reçu doit garantir avant d'être imprimé.
func (d Document) Check() error {
	if d.Kind == KindStatement {
		// Un relevé n'a pas de détail à additionner : ses lignes sont des
		// opérations, et son total est une somme calculée par l'appelant.
		return nil
	}
	sum := 0
	for _, l := range d.Lines {
		if l.NoAmount {
			continue
		}
		sum += l.Amount
	}
	if sum != d.Total {
		return fmt.Errorf("%w : détail %d, total %d", ErrLinesDoNotAddUp, sum, d.Total)
	}
	return nil
}

// Money formate un montant dans la monnaie du document.
//
// ⚠️ LES DÉCIMALES VIENNENT DE LA MONNAIE, pas d'un choix d'affichage. Un franc
// n'a pas de centimes et l'entier stocké EST l'unité ; un cedi en a deux, et
// l'entier stocké est en centièmes. Imprimer « 250000 GH₵ » là où il fallait
// « 2500.00 GH₵ » est une erreur de facteur cent sur un document qu'on présente.
func (d Document) Money(amount int) string {
	neg := amount < 0
	if neg {
		amount = -amount
	}
	var body string
	if d.Decimals <= 0 {
		body = group(strconv.Itoa(amount))
	} else {
		div := 1
		for i := 0; i < d.Decimals; i++ {
			div *= 10
		}
		body = group(strconv.Itoa(amount/div)) + "," +
			fmt.Sprintf("%0*d", d.Decimals, amount%div)
	}
	sym := d.CurrencySymbol
	if sym == "" {
		sym = d.CurrencyCode
	}
	out := body + " " + sym
	if neg {
		// Le signe moins AVANT le nombre, et pas des parenthèses : une remise
		// doit se lire comme une remise par quelqu'un qui n'est pas comptable.
		return "-" + out
	}
	return out
}

// group sépare les milliers par une espace insécable fine.
//
// ⚠️ UNE ESPACE INSÉCABLE, pour que « 2 500 » ne se coupe pas en fin de ligne —
// un montant coupé en deux sur un reçu se relit « 2 » puis « 500 ».
func group(s string) string {
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// Km formate une distance en mètres.
//
// ⚠️ UNE DÉCIMALE, ET JAMAIS LES MÈTRES NUS. « 8234 m » demande au lecteur de
// faire la conversion, et « 8,2 km » est ce que tout le monde dit. Sous un
// kilomètre, les mètres se lisent mieux : « 0,4 km » pour une course de quatre
// cents mètres a l'air d'une erreur.
func Km(m int) string {
	if m <= 0 {
		return "—"
	}
	if m < 1000 {
		return strconv.Itoa(m) + " m"
	}
	return strings.Replace(fmt.Sprintf("%.1f", float64(m)/1000), ".", ",", 1) + " km"
}

// Duration formate une durée en secondes, en heures et minutes.
//
// ⚠️ PAS DE SECONDES. Personne ne lit « 27 min 43 s » sur un reçu, et cette
// précision prétend une exactitude que la mesure n'a pas : la fin d'une course
// est l'instant où le chauffeur appuie sur un bouton.
func Duration(s int) string {
	if s <= 0 {
		return "—"
	}
	m := (s + 30) / 60
	if m < 60 {
		return strconv.Itoa(m) + " min"
	}
	return fmt.Sprintf("%d h %02d", m/60, m%60)
}

// Stamp formate un instant dans le fuseau donné.
//
// ⚠️ LE FUSEAU DU PAYS DE L'OPÉRATION, et c'est l'appelant qui le passe. Un reçu
// qui afficherait l'heure UTC daterait une course de 23 h 30 à Lomé du
// lendemain — et c'est la première chose qu'on vérifie en contestant un reçu.
func Stamp(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "—"
	}
	if loc != nil {
		t = t.In(loc)
	}
	return t.Format("02/01/2006 15:04")
}

// DayStamp formate une date sans heure — pour les bornes d'un relevé.
func DayStamp(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "—"
	}
	if loc != nil {
		t = t.In(loc)
	}
	return t.Format("02/01/2006")
}
