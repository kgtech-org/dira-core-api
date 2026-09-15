package country

// Currency décrit une monnaie telle qu'une application l'affiche.
//
// ⚠️ Les MONTANTS de la plateforme restent des entiers dans la plus petite
// unité de la monnaie du pays : un prix créé sous `GN` est en francs
// guinéens, un prix créé sous `TG` en francs CFA. La monnaie ne se convertit
// pas, elle se DÉCLARE — c'est le pays qui la porte, et c'est ce que
// `GET /countries` rend à l'application pour formater ce qu'elle affiche.
type Currency struct {
	Code string `json:"code"` // ISO 4217
	Name string `json:"name"`
	// Symbol est ce qu'on écrit après le montant : « 2 500 F CFA ».
	Symbol string `json:"symbol"`
	// Decimals est le nombre de décimales de la monnaie. Zéro pour les francs
	// — un montant est alors l'unité elle-même — et deux pour le cedi ou le
	// naira, où l'entier stocké est en centièmes.
	Decimals int `json:"decimals"`
}

// Currencies sont les monnaies des pays du catalogue, plus celles qu'un
// déploiement pourrait vouloir déclarer.
var Currencies = []Currency{
	{Code: "XOF", Name: "Franc CFA (UEMOA)", Symbol: "F CFA", Decimals: 0},
	{Code: "XAF", Name: "Franc CFA (CEMAC)", Symbol: "FCFA", Decimals: 0},
	{Code: "GNF", Name: "Franc guinéen", Symbol: "FG", Decimals: 0},
	{Code: "GHS", Name: "Cedi ghanéen", Symbol: "GH₵", Decimals: 2},
	{Code: "NGN", Name: "Naira", Symbol: "₦", Decimals: 2},
	{Code: "EUR", Name: "Euro", Symbol: "€", Decimals: 2},
	{Code: "USD", Name: "Dollar américain", Symbol: "$", Decimals: 2},
}

// LookupCurrency rend une monnaie connue.
func LookupCurrency(code string) (Currency, bool) {
	code = NormalizeCurrency(code)
	for _, c := range Currencies {
		if c.Code == code {
			return c, true
		}
	}
	return Currency{}, false
}

// NormalizeCurrency rend le code en majuscules, ou "" s'il n'a pas la forme
// d'un code ISO 4217.
func NormalizeCurrency(code string) string {
	c := ""
	for _, r := range code {
		switch {
		case r >= 'a' && r <= 'z':
			c += string(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z':
			c += string(r)
		case r == ' ':
		default:
			return ""
		}
	}
	if len(c) != 3 {
		return ""
	}
	return c
}
