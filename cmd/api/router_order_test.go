package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// ⚠️ CHI REFUSE UN MIDDLEWARE DÉCLARÉ APRÈS UNE ROUTE — PAR UNE PANIQUE AU
// DÉMARRAGE. Pas une erreur de compilation, pas un test qui rougit : le binaire
// se construit, l'image se publie, et le service refuse de démarrer une fois
// déployé. C'est arrivé à `dira-analytics` le 28 septembre 2026 :
// `r.Handle("/metrics", …)` posée entre deux `r.Use(…)`, conteneur en boucle de
// redémarrage, découvert au DÉPLOIEMENT parce que rien ne pouvait le dire avant.
// Ici l'ordre est correct — il l'était par chance, il l'est maintenant par
// contrôle.
//
// Ce test lit l'ordre dans la source. Un vrai test aurait à construire le
// routeur, donc Mongo, Redis, le modèle de langue et la messagerie — ce qui
// explique qu'il n'existe pas. L'ordre, lui, se lit sans rien démarrer.
func TestEveryMiddlewareIsDeclaredBeforeTheFirstRoute(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("lecture de main.go : %v", err)
	}

	router := routerName(file)
	if router == "" {
		t.Fatal("aucun `chi.NewRouter()` trouvé : ce test ne garde plus rien")
	}

	var lastUse, firstRoute token.Pos
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && isIdent(sel.X, router) {
			if sel.Sel.Name == "Use" {
				if call.Pos() > lastUse {
					lastUse = call.Pos()
				}
				return true
			}
			// ⚠️ `NotFound` et `MethodNotAllowed` NE SONT PAS des routes : chi
			// les garde à part et ne referme pas le routeur pour autant. Les
			// compter aurait fait échouer ce test sur un service qui va très
			// bien — et un garde-fou qui crie à tort finit par être supprimé,
			// emportant avec lui la panne qu'il savait attraper.
			switch sel.Sel.Name {
			case "NotFound", "MethodNotAllowed", "With":
				return true
			}
			// Toute autre méthode du routeur pose une route.
			if firstRoute == 0 || call.Pos() < firstRoute {
				firstRoute = call.Pos()
			}
			return true
		}
		// Et le routeur PASSÉ À QUELQU'UN D'AUTRE en pose aussi : `docs.Mount(r,
		// …)` monte de vraies routes, et chi ne fait pas la différence.
		for _, arg := range call.Args {
			if isIdent(arg, router) {
				if firstRoute == 0 || call.Pos() < firstRoute {
					firstRoute = call.Pos()
				}
			}
		}
		return true
	})

	if lastUse == 0 {
		t.Fatal("aucun `Use` sur le routeur : ce test ne garde plus rien")
	}
	if firstRoute != 0 && lastUse > firstRoute {
		t.Fatalf("middleware déclaré ligne %d, après la première route ligne %d — "+
			"chi paniquera AU DÉMARRAGE, et seul le déploiement le dira",
			fset.Position(lastUse).Line, fset.Position(firstRoute).Line)
	}
}

// routerName rend le nom de la variable qui reçoit `chi.NewRouter()`.
func routerName(file *ast.File) string {
	name := ""
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "NewRouter" || !isIdent(sel.X, "chi") {
			return true
		}
		if id, ok := assign.Lhs[0].(*ast.Ident); ok && name == "" {
			name = id.Name
		}
		return true
	})
	return name
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}
