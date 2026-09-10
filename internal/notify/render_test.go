package notify

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderReplacesVariables(t *testing.T) {
	out, missing := Render("Bonjour [client_name], votre commande [order_ref] est prête.",
		map[string]string{"client_name": "Awa", "order_ref": "A-42"})
	assert.Equal(t, "Bonjour Awa, votre commande A-42 est prête.", out)
	assert.Empty(t, missing)
}

// Une variable sans valeur laisse un VIDE et le message part quand même —
// choix produit. Mais elle est rendue à l'appelant : sans cela, le défaut ne
// se verrait nulle part, sinon chez le client.
func TestMissingVariableEmptiesButIsReported(t *testing.T) {
	out, missing := Render("Bonjour [client_name], commande [order_ref].",
		map[string]string{"order_ref": "A-42"})
	assert.Equal(t, "Bonjour , commande A-42.", out)
	assert.Equal(t, []string{"client_name"}, missing)
}

// Une variable manquante deux fois ne se signale qu'une fois : la liste sert à
// corriger un gabarit, pas à compter des occurrences.
func TestMissingVariableIsReportedOnce(t *testing.T) {
	_, missing := Render("[eta] et encore [eta] et [ref]", nil)
	assert.Equal(t, []string{"eta", "ref"}, missing)
}

// Un crochet que l'auteur voulait vraiment écrire doit survivre. Sans cette
// distinction, tout crochet deviendrait une variable et disparaîtrait.
func TestLiteralBracketsSurvive(t *testing.T) {
	cases := map[string]string{
		"Note [1] : voir plus bas":  "Note [1] : voir plus bas",
		"Barème [0-5] étoiles":      "Barème [0-5] étoiles",
		"Cliquez [voir ici]":        "Cliquez [voir ici]",
		"Crochet [ jamais refermé":  "Crochet [ jamais refermé",
		"Majuscules [ClientName] ?": "Majuscules [ClientName] ?",
	}
	for in, want := range cases {
		out, missing := Render(in, nil)
		assert.Equal(t, want, out, in)
		assert.Empty(t, missing, in)
	}
}

// Le crochet n'ouvre AUCUN formatage : une notification poussée n'affiche ni
// gras ni lien. Les balises restent LITTÉRALES plutôt que d'être effacées —
// visibles, donc corrigeables. `[b]` ressemble pourtant trait pour trait à une
// variable : c'est la règle des deux caractères minimum qui tranche.
func TestMarkupStaysLiteralAndIsNotMistakenForAVariable(t *testing.T) {
	out, missing := Render("[b]Gras[/b] et [url=http://x]lien[/url]", nil)
	assert.Equal(t, "[b]Gras[/b] et [url=http://x]lien[/url]", out)
	assert.Empty(t, missing, "aucune de ces balises n'est une variable")

	// La contrepartie assumée : personne ne peut nommer une variable d'une
	// seule lettre.
	out, missing = Render("[a] mais [ab]", map[string]string{"a": "X", "ab": "Y"})
	assert.Equal(t, "[a] mais Y", out)
	assert.Empty(t, missing)
}

// La valeur d'une variable n'est PAS ré-analysée : un client qui se nomme
// « [order_ref] » ne doit pas voir sa commande à la place de son nom.
func TestValuesAreNotReExpanded(t *testing.T) {
	out, _ := Render("Bonjour [client_name].",
		map[string]string{"client_name": "[order_ref]", "order_ref": "A-42"})
	assert.Equal(t, "Bonjour [order_ref].", out)
}

func TestEmptyTemplate(t *testing.T) {
	out, missing := Render("", map[string]string{"a": "b"})
	assert.Empty(t, out)
	assert.Empty(t, missing)
}

// Variables lit le gabarit avec le MÊME analyseur que le rendu : une
// extraction indépendante aurait fini par diverger, et l'écran
// d'administration montrerait des variables que l'envoi ignore.
func TestVariablesListsWhatTheTemplateExpects(t *testing.T) {
	vars := Variables("Commande [order_ref] de [store_name]", "Bonjour [client_name] — [store_name]")
	assert.Equal(t, []string{"client_name", "order_ref", "store_name"}, vars)
}

func TestTruncateKeepsRunesIntact(t *testing.T) {
	assert.Equal(t, "abc", Truncate("abc", 5))
	assert.Equal(t, "abcd…", Truncate("abcdefgh", 5))
	// Un texte accentué se coupe en CARACTÈRES, pas en octets : couper en
	// octets produirait un caractère invalide affiché en losange.
	assert.Equal(t, "éàè…", Truncate("éàèùô", 4))
}
