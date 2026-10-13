// Command check_api_docs verifies declaration comments on the public Go API.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check() error {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	fs := token.NewFileSet()
	total, missing := 0, 0
	checkComment := func(name string, pos token.Pos, comment *ast.CommentGroup) {
		total++
		if comment == nil || strings.TrimSpace(comment.Text()) == "" {
			fmt.Fprintf(os.Stderr, "%s: missing public API comment for %s\n", fs.Position(pos), name)
			missing++
		}
	}
	for _, dir := range []string{"kvp", "owscore", "wms", "wmts", "response", "server", "exception"} {
		paths, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		if err != nil {
			return err
		}
		if len(paths) == 0 {
			return fmt.Errorf("no source files in %s", dir)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fs, path, nil, parser.ParseComments)
			if err != nil {
				return err
			}
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if !d.Name.IsExported() {
						continue
					}
					if d.Recv != nil && !ast.IsExported(receiver(d.Recv.List[0].Type)) {
						continue
					}
					checkComment(d.Name.Name, d.Pos(), d.Doc)
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						if s, ok := spec.(*ast.TypeSpec); ok && s.Name.IsExported() {
							comment := s.Doc
							if comment == nil {
								comment = d.Doc
							}
							checkComment(s.Name.Name, s.Pos(), comment)
						}
					}
				}
			}
		}
	}
	if missing > 0 {
		return fmt.Errorf("%d of %d public declarations lack documentation", missing, total)
	}
	fmt.Printf("All %d public type/function/method declarations have GoDoc.\n", total)
	return nil
}

func receiver(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiver(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return receiver(e.X)
	case *ast.IndexListExpr:
		return receiver(e.X)
	}
	return ""
}
