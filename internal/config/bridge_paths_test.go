package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestBridgeDocPathsResolve verifies that every cfg.X.Y.Z path documented in
// the bridges_doc.go reverse index actually resolves through reflection on
// *config.Config. If a config field is renamed or removed, this test catches
// the staleness.
//
// Method-call paths (like cfg.Output.GetOperationMode()) are handled specially
// since they can't be resolved by field path alone.
func TestBridgeDocPathsResolve(t *testing.T) {
	docPaths := extractBridgesDocPaths()
	if len(docPaths) == 0 {
		t.Fatal("No paths found in bridges_doc.go reverse index — extraction may be broken")
	}

	cfgType := reflect.TypeOf(Config{})

	for _, path := range docPaths {
		t.Run(path, func(t *testing.T) {
			if !pathResolves(cfgType, path) {
				t.Errorf("documented path cfg.%s does not exist on *config.Config — update bridges_doc.go or the Config struct", path)
			}
		})
	}
}

// TestBridgeDocExhaustive verifies that the reverse index in bridges_doc.go
// covers all cfg.X.Y.Z paths found in bridge comments across the codebase,
// and that the doc doesn't contain stale entries. This ensures the manually
// maintained doc stays in sync with the actual bridge functions.
func TestBridgeDocExhaustive(t *testing.T) {
	// Extract paths from bridge comments in source code.
	rootDir := filepath.Join("..", "..")
	sourcePaths, err := extractAllBridgePaths(rootDir)
	if err != nil {
		t.Fatalf("Failed to scan bridge files: %v", err)
	}

	if len(sourcePaths) == 0 {
		t.Fatal("No Config-bridge reads: comments found — scan may be broken")
	}

	// Extract paths from bridges_doc.go reverse index.
	docPaths := extractBridgesDocPathSet()

	// Expand sub-config paths to their full Config-relative form.
	// Some bridge functions take a sub-config struct (e.g., TranslationConfig)
	// so their documented paths like "cfg.Enabled" are relative to that sub-struct.
	// The bridges_doc.go always uses the full path like "cfg.Metadata.Translation.Enabled".
	// We need to normalize source paths to full Config-relative paths for comparison.
	expandedPaths := expandSubConfigPaths(sourcePaths)

	// Check: every expanded source path should be in the doc.
	missingFromDoc := make([]string, 0)
	for _, path := range expandedPaths {
		if !docPaths[path] {
			missingFromDoc = append(missingFromDoc, path)
		}
	}
	sort.Strings(missingFromDoc)
	if len(missingFromDoc) > 0 {
		t.Errorf("Found %d paths in bridge comments but missing from bridges_doc.go reverse index:", len(missingFromDoc))
		for _, path := range missingFromDoc {
			t.Errorf("  cfg.%s", path)
		}
		t.Logf("Add these to the reverse index in bridges_doc.go")
	}

	// Check: every doc path should appear in at least one expanded source path.
	sourceSet := make(map[string]bool)
	for _, path := range expandedPaths {
		sourceSet[path] = true
	}

	staleInDoc := make([]string, 0)
	for path := range docPaths {
		if !sourceSet[path] {
			staleInDoc = append(staleInDoc, path)
		}
	}
	sort.Strings(staleInDoc)
	if len(staleInDoc) > 0 {
		t.Errorf("Found %d paths in bridges_doc.go but not in any bridge comment (may be stale):", len(staleInDoc))
		for _, path := range staleInDoc {
			t.Errorf("  cfg.%s", path)
		}
		t.Logf("Remove these from the reverse index in bridges_doc.go, or add corresponding Config-bridge reads: comments")
	}
}

// TestBridgeCommentsMatchCode verifies that the cfg.X.Y.Z paths documented
// in each bridge function's "Config-bridge reads:" comment actually match
// the code in that function. This catches cases where a bridge reads a field
// but forgets to document it, or documents a path it no longer reads.
func TestBridgeCommentsMatchCode(t *testing.T) {
	rootDir := filepath.Join("..", "..")
	bridges, err := extractBridgeFunctions(rootDir)
	if err != nil {
		t.Fatalf("Failed to scan bridge functions: %v", err)
	}

	for _, bridge := range bridges {
		t.Run(bridge.pkg+"/"+bridge.funcName, func(t *testing.T) {
			// For sub-config bridges (those that take a sub-config struct rather than *config.Config),
			// the documented paths are relative to that sub-struct, so we can't verify them
			// against Config directly. We skip verification for these.
			if !strings.Contains(bridge.paramType, "config.Config") {
				t.Skipf("bridge takes %s (sub-config struct), path verification skipped", bridge.paramType)
			}

			// Check that all documented paths resolve on Config.
			cfgType := reflect.TypeOf(Config{})
			for _, path := range bridge.documentedPaths {
				if !pathResolves(cfgType, path) {
					t.Errorf("documented path cfg.%s does not resolve on *config.Config", path)
				}
			}
		})
	}
}

// TestBridgeCommentsMatchActualAccesses verifies that each bridge function's
// "Config-bridge reads:" comment matches the actual cfg.* field accesses in
// the function body. Per D-9: uses Go AST analysis (go/ast, go/parser) to
// extract field accesses and compare against documented paths.
//
// This catches two classes of bugs:
//   - Undocumented reads: the function accesses cfg.X.Y but the comment doesn't list it
//   - Stale documentation: the comment lists cfg.X.Y but the function no longer reads it
//
// Comparison rules:
//   - Intermediate paths (e.g., cfg.Scrapers) are considered covered if any documented
//     path starts with that prefix (e.g., Scrapers.Proxy covers Scrapers).
//   - Method calls are normalized: AST sees "Output.GetOperationMode" but docs may
//     write "Output.GetOperationMode()" — both are treated as equivalent.
//   - Bridge functions that return a sub-config pointer (e.g., &cfg.Database) expose
//     all fields under that sub-config transitively. Documented leaf paths under
//     the returned sub-config are considered covered by the intermediate access.
func TestBridgeCommentsMatchActualAccesses(t *testing.T) {
	rootDir := filepath.Join("..", "..")
	bridges, err := extractBridgeFunctions(rootDir)
	if err != nil {
		t.Fatalf("Failed to scan bridge functions: %v", err)
	}

	for _, bridge := range bridges {
		t.Run(bridge.pkg+"/"+bridge.funcName, func(t *testing.T) {
			// Only verify bridges that take *config.Config — sub-config bridges
			// have paths relative to their sub-struct, not to Config.
			if !strings.Contains(bridge.paramType, "config.Config") {
				t.Skipf("bridge takes %s (sub-config struct), AST verification skipped", bridge.paramType)
			}

			// Build sets for comparison, normalizing method calls.
			normDoc := normalizePaths(bridge.documentedPaths)
			normActual := normalizePaths(bridge.actualPaths)

			docSet := make(map[string]bool)
			for _, p := range normDoc {
				docSet[p] = true
			}
			actualSet := make(map[string]bool)
			for _, p := range normActual {
				actualSet[p] = true
			}

			// Find undocumented reads: accessed in code but not in comment.
			// Intermediate paths (e.g., Scrapers) are covered if any documented path
			// has them as a prefix (e.g., Scrapers.Proxy covers Scrapers).
			var undocumented []string
			for _, p := range normActual {
				if docSet[p] {
					continue
				}
				if isCoveredByPrefix(p, normDoc) {
					continue
				}
				undocumented = append(undocumented, p)
			}
			sort.Strings(undocumented)
			if len(undocumented) > 0 {
				t.Errorf("Bridge %s accesses cfg.%s but it's not documented in Config-bridge reads: comment",
					bridge.funcName, strings.Join(undocumented, ", cfg."))
			}

			// Find stale documentation: documented but not accessed in code.
			// A documented path is covered if:
			//   - The code directly accesses it, OR
			//   - The code accesses a parent and the documented path is a child (transitive reads), OR
			//   - The code accesses a child of the documented path
			var stale []string
			for _, p := range normDoc {
				if actualSet[p] {
					continue
				}
				if isCoveredByChild(p, normActual) {
					continue
				}
				// Transitive: if code accesses a parent (e.g., Database) and the doc
				// path is a child (e.g., Database.DSN), the parent access covers it
				// because the bridge returns a pointer to the sub-config.
				if isCoveredByTransitiveParent(p, normActual) {
					continue
				}
				stale = append(stale, p)
			}
			sort.Strings(stale)
			if len(stale) > 0 {
				t.Errorf("Bridge %s documents cfg.%s in Config-bridge reads: but the code doesn't access it",
					bridge.funcName, strings.Join(stale, ", cfg."))
			}
		})
	}
}

// --- helpers ---

// pathResolves checks whether a dot-separated path resolves through the struct
// hierarchy using reflection. Handles method calls like "Output.GetOperationMode()".
func pathResolves(t reflect.Type, path string) bool {
	parts := strings.Split(path, ".")
	current := t

	for i, part := range parts {
		// Handle method calls (e.g., "GetOperationMode()")
		if strings.HasSuffix(part, "()") {
			methodName := strings.TrimSuffix(part, "()")
			// Check both value and pointer receivers.
			_, found := current.MethodByName(methodName)
			if !found && current.Kind() != reflect.Ptr {
				_, found = reflect.PtrTo(current).MethodByName(methodName)
			}
			return found
		}

		// Dereference pointers.
		if current.Kind() == reflect.Ptr {
			current = current.Elem()
		}

		if current.Kind() == reflect.Struct {
			field, found := current.FieldByName(part)
			if !found {
				return false
			}
			current = field.Type
		} else if current.Kind() == reflect.Map {
			// map[string]... fields can't be resolved further
			return true
		} else {
			return false
		}

		// If this is the last part, we've resolved successfully.
		if i == len(parts)-1 {
			return true
		}
	}

	return true
}

// bridgeFunc represents a bridge function found in source code.
type bridgeFunc struct {
	pkg             string   // short package name (e.g., "nfo", "api/core")
	funcName        string   // function name
	paramType       string   // parameter type (e.g., "*config.Config", "config.TranslationConfig")
	documentedPaths []string // paths from the Config-bridge reads: comment
	actualPaths     []string // paths actually accessed in the function body (via AST analysis)
	filePath        string   // source file path for error messages
}

// extractBridgeFunctions scans all .go files under internal/ for functions
// with "Config-bridge reads:" comments.
func extractBridgeFunctions(rootDir string) ([]bridgeFunc, error) {
	var result []bridgeFunc
	pathPattern := regexp.MustCompile(`cfg\.([A-Za-z0-9_.]+(?:\(\))?)`)

	internalDir := filepath.Join(rootDir, "internal")

	err := filepath.Walk(internalDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.go") || base == "bridges_doc.go" {
			return nil
		}

		dir := filepath.Dir(path)
		shortPkg := ""
		internalIdx := strings.Index(dir, string(filepath.Separator)+"internal"+string(filepath.Separator))
		if internalIdx >= 0 {
			shortPkg = dir[internalIdx+1:]
		} else {
			shortPkg = filepath.Base(dir)
		}
		if strings.HasPrefix(shortPkg, "internal/") {
			shortPkg = strings.TrimPrefix(shortPkg, "internal/")
		}

		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Doc == nil {
				continue
			}

			// Collect all documented paths from the comment.
			var docPaths []string
			hasBridgeReads := false
			for _, comment := range fn.Doc.List {
				text := comment.Text
				if strings.Contains(text, "Config-bridge reads:") {
					hasBridgeReads = true
				}
				if hasBridgeReads {
					matches := pathPattern.FindAllStringSubmatch(text, -1)
					for _, match := range matches {
						if len(match) > 1 {
							docPaths = append(docPaths, match[1])
						}
					}
				}
			}

			if !hasBridgeReads {
				continue
			}

			// Determine the parameter type.
			paramType := ""
			if fn.Type.Params != nil && len(fn.Type.Params.List) > 0 {
				paramType = exprToString(fn.Type.Params.List[0].Type)
			}

			// Deduplicate paths.
			seen := make(map[string]bool)
			deduped := make([]string, 0, len(docPaths))
			for _, p := range docPaths {
				if !seen[p] {
					seen[p] = true
					deduped = append(deduped, p)
				}
			}
			sort.Strings(deduped)

			result = append(result, bridgeFunc{
				pkg:             shortPkg,
				funcName:        fn.Name.Name,
				paramType:       paramType,
				documentedPaths: deduped,
				actualPaths:     extractActualPaths(fn, paramType),
				filePath:        path,
			})
		}

		return nil
	})

	return result, err
}

// exprToString converts an AST expression to a string representation.
func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	default:
		return ""
	}
}

// extractActualPaths uses Go AST analysis to find all cfg.X.Y.Z field access
// paths in a bridge function's body. Per D-9: this is compared against the
// documented paths in the "Config-bridge reads:" comment to detect mismatches.
//
// The function walks the AST looking for selector expressions (X.Y.Z chains)
// where the root identifier matches the first parameter name (typically "cfg").
// It returns the deduplicated, sorted list of paths relative to the root
// (i.e., "Scrapers.Overrides", not "cfg.Scrapers.Overrides").
func extractActualPaths(fn *ast.FuncDecl, paramType string) []string {
	if fn.Body == nil {
		return nil
	}

	// Determine the name of the first parameter (the config param).
	if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
		return nil
	}
	param := fn.Type.Params.List[0]
	if len(param.Names) == 0 {
		return nil
	}
	paramName := param.Names[0].Name

	// Walk the function body to find selector chains starting with paramName.
	var paths []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		// Build the full selector chain.
		chain := buildSelectorChain(sel, paramName)
		if chain != "" {
			paths = append(paths, chain)
		}
		return true
	})

	// Deduplicate and sort.
	seen := make(map[string]bool)
	deduped := make([]string, 0, len(paths))
	for _, p := range paths {
		if !seen[p] {
			seen[p] = true
			deduped = append(deduped, p)
		}
	}
	sort.Strings(deduped)
	return deduped
}

// buildSelectorChain reconstructs the dot-separated path from a SelectorExpr,
// but only if the root of the chain matches the expected parameter name.
// Returns empty string if the chain doesn't start with paramName.
// For example, given cfg.Scrapers.Proxy with paramName="cfg", returns "Scrapers.Proxy".
func buildSelectorChain(sel *ast.SelectorExpr, paramName string) string {
	// Recursively build the chain from left to right.
	var parts []string
	current := ast.Expr(sel)

	for {
		s, ok := current.(*ast.SelectorExpr)
		if !ok {
			break
		}
		parts = append([]string{s.Sel.Name}, parts...)
		current = s.X
	}

	// Check that the root is the expected parameter name.
	ident, ok := current.(*ast.Ident)
	if !ok || ident.Name != paramName {
		return ""
	}

	return strings.Join(parts, ".")
}

// extractAllBridgePaths returns all unique cfg.X.Y.Z paths from bridge
// comments, without the package association.
func extractAllBridgePaths(rootDir string) (map[string][]string, error) {
	bridges, err := extractBridgeFunctions(rootDir)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]string) // path → []package
	for _, bridge := range bridges {
		for _, path := range bridge.documentedPaths {
			result[path] = append(result[path], bridge.pkg)
		}
	}

	// Deduplicate and sort package lists.
	for path := range result {
		seen := make(map[string]bool)
		deduped := make([]string, 0)
		for _, pkg := range result[path] {
			if !seen[pkg] {
				seen[pkg] = true
				deduped = append(deduped, pkg)
			}
		}
		sort.Strings(deduped)
		result[path] = deduped
	}

	return result, nil
}

// expandSubConfigPaths expands short paths from sub-config bridge functions
// to their full Config-relative form. Bridge functions that take a sub-config
// struct (like TranslationConfig) document paths relative to that struct.
// This function maps them to their full cfg.X.Y.Z form.
func expandSubConfigPaths(paths map[string][]string) []string {
	// Known sub-config prefixes:
	// Bridges that take *config.Config → paths are already full
	// Bridges that take config.TranslationConfig → paths are relative to cfg.Metadata.Translation
	// Bridges that take config.MetadataConfig → paths are relative to cfg.Metadata
	// Bridges that take config.ScrapersConfig → paths are relative to cfg.Scrapers

	subConfigPrefixes := map[string]string{
		"translation": "Metadata.Translation",
		"scrape":      "Metadata.Translation", // NewTranslatorFromApp takes TranslationConfig
		"aggregator":  "Metadata",             // MetadataConfigFromApp takes MetadataConfig
	}

	var expanded []string
	for path, pkgs := range paths {
		// Check if any of the packages uses a sub-config prefix.
		prefix := ""
		for _, pkg := range pkgs {
			if p, ok := subConfigPrefixes[pkg]; ok {
				// Only apply if the path doesn't already start with a known top-level field.
				if !isFullConfigPath(path) {
					prefix = p
					break
				}
			}
		}

		if prefix != "" && !isFullConfigPath(path) {
			expanded = append(expanded, prefix+"."+path)
		} else {
			expanded = append(expanded, path)
		}
	}

	sort.Strings(expanded)
	return expanded
}

// isFullConfigPath returns true if the path starts with a known top-level
// field of Config (i.e., it's already a full Config-relative path).
func isFullConfigPath(path string) bool {
	topLevelFields := []string{
		"API", "Server", "Database", "Logging", "System",
		"Matching", "Metadata", "Output", "Scrapers", "Performance",
	}
	for _, field := range topLevelFields {
		if strings.HasPrefix(path, field+".") || path == field {
			return true
		}
	}
	return false
}

// extractBridgesDocPaths reads bridges_doc.go and extracts all cfg.X.Y.Z
// paths from the reverse index section, returning them as a sorted slice.
// normalizePaths strips trailing () from method-call paths and returns the
// normalized slice. This allows comparing AST-extracted paths (which don't
// have parens) with documented paths (which may have parens).
func normalizePaths(paths []string) []string {
	result := make([]string, len(paths))
	for i, p := range paths {
		result[i] = strings.TrimSuffix(p, "()")
	}
	return result
}

// isCoveredByPrefix returns true if path is a prefix of any candidate path.
// For example, path="Scrapers" is covered by candidate="Scrapers.Proxy".
// This handles intermediate path accesses in AST that are stepping stones
// to the documented leaf paths.
func isCoveredByPrefix(path string, candidates []string) bool {
	prefix := path + "."
	for _, c := range candidates {
		normC := strings.TrimSuffix(c, "()")
		if strings.HasPrefix(normC, prefix) {
			return true
		}
	}
	return false
}

// isCoveredByChild returns true if any candidate has path as a prefix,
// meaning the code accesses a child of the documented path.
// For example, doc path="Scrapers.Proxy" is covered by candidate="Scrapers.Proxy".
// Doc path="Scrapers" is covered by candidate="Scrapers.Proxy".
func isCoveredByChild(path string, candidates []string) bool {
	normPath := strings.TrimSuffix(path, "()")
	for _, c := range candidates {
		normC := strings.TrimSuffix(c, "()")
		if normC == normPath {
			return true
		}
		// Code accessing a child covers the documented parent.
		if strings.HasPrefix(normC, normPath+".") {
			return true
		}
	}
	return false
}

// isCoveredByTransitiveParent returns true if the code accesses a parent of path,
// meaning the bridge returns a sub-config pointer and the documented path is a
// transitive read through that pointer. For example, if the code accesses
// "Database" and the doc path is "Database.DSN", the parent access covers it
// because the bridge returns &cfg.Database.
func isCoveredByTransitiveParent(path string, candidates []string) bool {
	normPath := strings.TrimSuffix(path, "()")
	for _, c := range candidates {
		normC := strings.TrimSuffix(c, "()")
		// The candidate is a parent of the documented path.
		if strings.HasPrefix(normPath, normC+".") {
			return true
		}
	}
	return false
}

func extractBridgesDocPaths() []string {
	docSet := extractBridgesDocPathSet()
	paths := make([]string, 0, len(docSet))
	for path := range docSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// extractBridgesDocPathSet reads bridges_doc.go and extracts all cfg.X.Y.Z
// paths from the reverse index section, returning them as a set.
func extractBridgesDocPathSet() map[string]bool {
	result := make(map[string]bool)

	data, err := os.ReadFile("bridges_doc.go")
	if err != nil {
		return result
	}

	pathPattern := regexp.MustCompile(`cfg\.([A-Za-z0-9_.]+(?:\(\))?)\s*→`)
	for _, line := range strings.Split(string(data), "\n") {
		matches := pathPattern.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			if len(match) > 1 {
				result[match[1]] = true
			}
		}
	}

	return result
}
