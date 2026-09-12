// Package phone canonicalise les numéros de téléphone en E.164.
//
// Le téléphone est l'IDENTIFIANT d'un compte : deux écritures du même numéro
// sont deux comptes, et la personne ne retrouve plus le sien à la connexion.
// La règle est donc unique, ici, et chaque porte d'entrée — inscription,
// connexion, console, surface de service — la traverse avant de chercher ou
// d'écrire.
package phone

import (
	"errors"
	"strings"
)

// ErrInvalid dit qu'aucune lecture raisonnable ne fait de la chaîne un
// numéro international.
var ErrInvalid = errors.New("phone: not an E.164 number")

// Normalize rend la forme canonique `+<indicatif><numéro>` (8 à 15 chiffres).
//
// Tolérant sur la FORME — espaces, points, tirets, parenthèses, le `00`
// international — et strict sur le FOND : sans indicatif, on ne devine pas.
// « 99000001 » peut être togolais ou béninois ; l'écrire « +99000001 » serait
// inventer un pays. Une application qui pré-remplit `+228` fait ce choix à
// l'endroit où la personne peut le voir.
func Normalize(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "00") {
		s = "+" + s[2:]
	}
	if !strings.HasPrefix(s, "+") {
		return "", ErrInvalid
	}
	var b strings.Builder
	b.WriteByte('+')
	for _, r := range s[1:] {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '.' || r == '-' || r == '(' || r == ')':
			// séparateurs de confort : ignorés
		default:
			return "", ErrInvalid
		}
	}
	digits := b.Len() - 1
	if digits < 8 || digits > 15 || b.String()[1] == '0' {
		return "", ErrInvalid
	}
	return b.String(), nil
}

// Valid dit si Normalize accepterait la chaîne. C'est la règle du tag de
// validation `e164` : celle de la bibliothèque rendait le `+` FACULTATIF, et
// « 22899000001 » passait — puis était stocké tel quel, à côté de
// « +22899000001 ».
func Valid(raw string) bool {
	_, err := Normalize(raw)
	return err == nil
}
