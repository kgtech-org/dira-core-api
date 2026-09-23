// Package guard relit les sources d'un service et dit quelles LECTURES Mongo
// d'une collection bornée par pays ne portent pas leur pays.
//
// ⚠️ IL EXISTE PARCE QU'UN OUBLI NE SE VOIT PAS. `country.Restrict` absent
// d'une agrégation ne casse rien : la requête répond, les chiffres sont
// plausibles, et la console de Dakar affiche l'activité de Lomé. C'est
// exactement ce qui est arrivé au bandeau du tableau de bord — deux
// agrégations sur trois bornées, la troisième non, et personne ne pouvait le
// voir sans relire les trois.
//
// La règle est simple, et c'est ce qui la rend vérifiable :
//
//	une lecture d'une collection bornée par pays est bornée par le PAYS,
//	ou par un IDENTIFIANT — sinon elle est signalée.
//
// Une lecture par identifiant (`_id`, `order_id`, `driver_id`…) désigne un
// objet précis : elle est déjà bornée, puisque l'objet porte son pays. Une
// lecture sans l'un ni l'autre balaye la collection entière, et c'est
// toujours une fuite ou un balayage de fond. Les balayages de fond
// s'inscrivent dans `Options.Global`, avec leur raison : ils sont rares, et
// les écrire une fois vaut mieux que de les redécouvrir à chaque relecture.
package guard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// readers : les lectures Mongo, et la place du filtre dans leurs arguments.
var readers = map[string]int{
	"Find": 1, "FindOne": 1, "Aggregate": 1, "CountDocuments": 1, "Distinct": 2,
}

// Options décrit ce que le service veut faire vérifier.
type Options struct {
	// Collections : les champs de dépôt qui portent une collection dont les
	// documents ont un `country` — `rides`, `orders`, `merchants`. Le nom du
	// CHAMP, tel qu'il est écrit dans le code (`r.rides.Find(...)`).
	//
	// `paquet.champ` quand le nom est trop banal pour être unique : plusieurs
	// dépôts appellent `col` leur unique collection, et une seule d'entre
	// elles porte un pays.
	Collections []string
	// Global : les lectures légitimement sans pays, « paquet.Fonction » →
	// la raison. Un balayage de fond n'a pas de requête, donc pas de pays :
	// `country.Restrict` y serait sans effet, et l'écrire ferait croire à une
	// borne qui n'existe pas.
	Global map[string]string
}

// Finding est une lecture qui ne porte pas son pays.
type Finding struct {
	File       string // relatif à la racine scannée
	Line       int
	Package    string
	Func       string
	Collection string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d  %s.%s lit `%s` sans pays", f.File, f.Line, f.Package, f.Func, f.Collection)
}

// Key est le nom sous lequel une lecture s'inscrit dans `Options.Global`.
func (f Finding) Key() string { return f.Package + "." + f.Func }

// Scan relit l'arborescence et rend les lectures non bornées, dans l'ordre
// des fichiers. Les fichiers de test sont ignorés : ils n'ont pas de pays.
func Scan(root string, opts Options) ([]Finding, error) {
	guarded := make(map[string]bool, len(opts.Collections))
	for _, c := range opts.Collections {
		guarded[strings.TrimSpace(c)] = true
	}
	files, err := collect(root)
	if err != nil {
		return nil, err
	}
	// Une arborescence vide est une ERREUR, pas un feu vert : un analyseur
	// qui n'a rien lu ne dit rien, et son silence se lit comme « tout va
	// bien ».
	if len(files) == 0 {
		return nil, fmt.Errorf("guard: aucune source Go sous %q", root)
	}
	fset := token.NewFileSet()
	// Premier passage : toutes les fonctions, par paquet. Un filtre est
	// souvent construit par une méthode voisine (`r.window(ctx, p)`), et la
	// borne est là-bas.
	helpers := map[string]map[string]*ast.FuncDecl{}
	parsed := make([]*ast.File, len(files))
	for i, path := range files {
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil, fmt.Errorf("guard: %s: %w", path, perr)
		}
		parsed[i] = f
		dir := filepath.Dir(path)
		if helpers[dir] == nil {
			helpers[dir] = map[string]*ast.FuncDecl{}
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				helpers[dir][fn.Name.Name] = fn
			}
		}
	}

	var out []Finding
	for i, f := range parsed {
		path := files[i]
		rel, _ := filepath.Rel(root, path)
		local := helpers[filepath.Dir(path)]
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				at, isRead := readers[sel.Sel.Name]
				if !isRead || at >= len(call.Args) {
					return true
				}
				col, ok := sel.X.(*ast.SelectorExpr)
				if !ok || !(guarded[col.Sel.Name] || guarded[f.Name.Name+"."+col.Sel.Name]) {
					return true
				}
				if bounded(fn, local, call.Args[at], sel.Sel.Name == "Aggregate") {
					return true
				}
				out = append(out, Finding{
					File: rel, Line: fset.Position(call.Pos()).Line,
					Package: f.Name.Name, Func: fn.Name.Name, Collection: col.Sel.Name,
				})
				return true
			})
		}
	}
	// Ce que le service a déclaré global sort de la liste.
	kept := out[:0]
	for _, f := range out {
		if _, ok := opts.Global[f.Key()]; !ok {
			kept = append(kept, f)
		}
	}
	return kept, nil
}

// Stale rend les exemptions de `Global` qui ne correspondent plus à aucune
// lecture — une borne posée depuis, une fonction disparue. Une exemption
// périmée finit par couvrir autre chose que ce qu'on avait relu.
func Stale(root string, opts Options) ([]string, error) {
	all, err := Scan(root, Options{Collections: opts.Collections})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, f := range all {
		seen[f.Key()] = true
	}
	var stale []string
	for k := range opts.Global {
		if !seen[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(stale)
	return stale, nil
}

func collect(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// La racine elle-même n'est jamais écartée : elle s'appelle
			// souvent `..` ou `.`, et l'écarter ne scannait rien du tout —
			// un analyseur qui ne trouve rien passe pour un feu vert.
			if path == root {
				return nil
			}
			if d.Name() == "vendor" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// bounded dit si le filtre porte le pays ou un identifiant.
func bounded(fn *ast.FuncDecl, local map[string]*ast.FuncDecl, arg ast.Expr, pipeline bool) bool {
	if isParam(fn, arg) {
		// Le filtre vient de l'APPELANT : c'est lui qui le borne, et c'est lui
		// qu'on relit. Un assistant de pagination ne peut pas savoir sur quoi
		// il pagine.
		return true
	}
	for _, e := range resolve(fn, arg) {
		if followsHelper(local, e) {
			return true
		}
		// `country.Restrict` ne peut être QUE dans une position de filtre :
		// le trouver n'importe où dans le pipeline suffit.
		if contains(e, isRestrict) {
			return true
		}
		if pipeline {
			e = matchStages(e)
			if e == nil {
				continue
			}
		}
		if hasIDBound(e) {
			return true
		}
	}
	return false
}

// isParam dit si l'expression est un paramètre de la fonction.
func isParam(fn *ast.FuncDecl, arg ast.Expr) bool {
	id, ok := arg.(*ast.Ident)
	if !ok || fn.Type.Params == nil {
		return false
	}
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if name.Name == id.Name {
				return true
			}
		}
	}
	return false
}

// followsHelper suit d'un cran un filtre construit par une fonction voisine.
func followsHelper(local map[string]*ast.FuncDecl, e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return !found
		}
		name := ""
		switch f := call.Fun.(type) {
		case *ast.Ident:
			name = f.Name
		case *ast.SelectorExpr:
			name = f.Sel.Name
		}
		if h, ok := local[name]; ok && contains(h.Body, isRestrict) {
			found = true
		}
		return !found
	})
	return found
}

// resolve rend l'expression du filtre et TOUT ce qui la construit :
// `filter := country.Restrict(…)` puis `filter["status"] = …`, un filtre
// borné puis emballé — `beforeCursor(filter, cursor)` —, ou un pipeline qui
// cite une variable bornée plus haut (`"query": query`).
//
// La remontée s'arrête au bout de quelques crans : elle sert à retrouver une
// borne, pas à prouver un programme.
func resolve(fn *ast.FuncDecl, arg ast.Expr) []ast.Expr {
	out := []ast.Expr{arg}
	seen := map[string]bool{}
	for depth := 0; depth < 3; depth++ {
		var next []ast.Expr
		for _, e := range out {
			ast.Inspect(e, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || seen[id.Name] {
					return true
				}
				seen[id.Name] = true
				next = append(next, assignedTo(fn, id.Name)...)
				return true
			})
		}
		if len(next) == 0 {
			break
		}
		out = append(out, next...)
	}
	return out
}

// assignedTo rend tout ce qui est affecté à une variable dans le corps :
// sa construction, et les clés posées ensuite.
func assignedTo(fn *ast.FuncDecl, name string) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		s, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range s.Lhs {
			switch l := lhs.(type) {
			case *ast.Ident:
				if l.Name == name && i < len(s.Rhs) {
					out = append(out, s.Rhs[i])
				}
			case *ast.IndexExpr:
				// `filter["_id"] = bson.M{"$lt": …}` : la CLÉ et sa VALEUR
				// voyagent ensemble, sinon on ne peut pas distinguer une borne
				// (`"_id": id`) d'un curseur de pagination (`"_id": {$lt: …}`).
				if x, ok := l.X.(*ast.Ident); ok && x.Name == name && i < len(s.Rhs) {
					out = append(out, &ast.KeyValueExpr{Key: l.Index, Value: s.Rhs[i]})
				}
			}
		}
		return true
	})
	return out
}

// matchStages isole les étages `$match` d'un pipeline. Le reste en porte
// aussi, des `_id` : celui d'un `$group` NOMME le regroupement, il ne borne
// rien — le prendre pour une borne laisserait passer toutes les agrégations.
func matchStages(e ast.Expr) ast.Expr {
	var stages []ast.Expr
	ast.Inspect(e, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		switch k := kv.Key.(type) {
		case *ast.BasicLit:
			// `query` est le filtre d'un `$geoNear`, qui doit être le premier
			// étage et n'admet donc pas de `$match` avant lui.
			switch strings.Trim(k.Value, `"`) {
			case "$match", "query":
				stages = append(stages, kv.Value)
			}
		case *ast.Ident:
			// bson.E{Key: "$match", Value: …} — la valeur suit la clé.
			if k.Name == "Value" {
				stages = append(stages, kv.Value)
			}
		}
		return true
	})
	if len(stages) == 0 {
		return nil
	}
	return &ast.CompositeLit{Elts: stages}
}

func contains(n ast.Node, pred func(ast.Node) bool) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		if x != nil && pred(x) {
			found = true
		}
		return !found
	})
	return found
}

func isRestrict(n ast.Node) bool {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == "country" && (sel.Sel.Name == "Restrict" || sel.Sel.Name == "RestrictD")
}

// hasIDBound dit si le filtre désigne des objets PRÉCIS par leur
// identifiant — `{"_id": id}`, `{"order_id": {"$in": ids}}`.
//
// ⚠️ UN CURSEUR N'EST PAS UNE BORNE. `{"_id": {"$lt": cursor}}` pagine ; il
// dit « les suivants », pas « ceux-ci ». Le confondre avec une borne aurait
// donné un feu vert à toutes les listes paginées de la plateforme — c'est-à-dire
// à presque toutes.
func hasIDBound(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(x ast.Node) bool {
		kv, ok := x.(*ast.KeyValueExpr)
		if !ok {
			// Une clé d'identifiant sans sa valeur (un filtre monté ailleurs) :
			// on la croit sur parole.
			if lit, ok := x.(*ast.BasicLit); ok && isIDName(lit) && !underKeyValue(n, lit) {
				found = true
			}
			return !found
		}
		lit, ok := kv.Key.(*ast.BasicLit)
		if !ok || !isIDName(lit) {
			return true
		}
		if !contains(kv.Value, isRangeOp) {
			found = true
		}
		return !found
	})
	return found
}

// underKeyValue dit si ce littéral est la clé d'une paire — auquel cas la
// paire l'a déjà jugé, avec sa valeur.
func underKeyValue(root ast.Node, lit *ast.BasicLit) bool {
	under := false
	ast.Inspect(root, func(x ast.Node) bool {
		if kv, ok := x.(*ast.KeyValueExpr); ok && kv.Key == ast.Expr(lit) {
			under = true
		}
		return !under
	})
	return under
}

func isIDName(lit *ast.BasicLit) bool {
	if lit.Kind != token.STRING {
		return false
	}
	v := strings.Trim(lit.Value, `"`)
	return v == "_id" || strings.HasSuffix(v, "_id") || strings.HasSuffix(v, "_ids")
}

func isRangeOp(n ast.Node) bool {
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	switch strings.Trim(lit.Value, `"`) {
	case "$lt", "$lte", "$gt", "$gte":
		return true
	}
	return false
}
