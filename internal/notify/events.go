package notify

import (
	"context"
	"fmt"
	"strconv"
)

// Notifier est ce que les autres modules consomment. Ils déclarent CETTE
// interface chez eux ; le câblage y injecte le service.
//
// Volontairement SANS ERREUR : aucune opération métier ne doit échouer parce
// qu'une notification n'est pas partie. Une commande acceptée dont le client
// n'a pas été prévenu reste une commande acceptée.
type Notifier interface {
	Notify(ctx context.Context, userID, key string, vars, data map[string]string)
}

var _ Notifier = (*Service)(nil)

// OrderStatusKey traduit un statut de commande en clé de message.
//
// Tous les statuts n'en ont pas, et c'est voulu : `pending_payment` ne se
// notifie pas — le client est devant son écran de paiement. Une notification
// par transition apprendrait à couper les notifications de l'application.
func OrderStatusKey(status string) (string, bool) {
	switch status {
	case "paid":
		return KeyOrderConfirmed, true
	case "preparing":
		return KeyOrderPreparing, true
	case "ready":
		return KeyOrderReady, true
	case "assigned":
		return KeyOrderAssigned, true
	case "delivered":
		return KeyOrderDelivered, true
	case "cancelled":
		return KeyOrderCancelled, true
	default:
		return "", false
	}
}

// OrderVars compose les variables d'un message de commande.
//
// `order_ref` est un COURT extrait de l'identifiant, pas l'identifiant entier :
// vingt-quatre caractères hexadécimaux dans une notification ne désignent rien
// pour le client, et mangent la place du reste de la phrase.
func OrderVars(orderID, storeName string) map[string]string {
	return map[string]string{
		"order_ref":  ShortRef(orderID),
		"store_name": storeName,
	}
}

// OrderData est ce qui permet à l'application d'ouvrir la BONNE commande
// plutôt que l'écran d'accueil.
func OrderData(orderID, kind string) map[string]string {
	return map[string]string{"type": kind, "order_id": orderID}
}

// DriverCallVars compose les variables d'un appel de course.
//
// `cash_line` est une phrase entière et non un montant : en ligne il n'y a
// rien à avancer, et laisser « 0 FCFA à avancer » dans la notification ferait
// hésiter un livreur pour rien.
func DriverCallVars(distanceM, tokenCost, cashXOF int) map[string]string {
	cashLine := ""
	if cashXOF > 0 {
		cashLine = fmt.Sprintf(" · %d FCFA à avancer", cashXOF)
	}
	return map[string]string{
		"distance_km": strconv.FormatFloat(float64(distanceM)/1000, 'f', 1, 64),
		"token_cost":  strconv.Itoa(tokenCost),
		"cash_line":   cashLine,
	}
}

// ShortRef rend la référence courte d'un identifiant, telle que le client la
// lit sur son écran.
func ShortRef(id string) string {
	if len(id) <= 6 {
		return upper(id)
	}
	return upper(id[len(id)-6:])
}

func upper(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'a' && c <= 'z' {
			out[i] = c - ('a' - 'A')
		}
	}
	return string(out)
}
