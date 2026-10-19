package r18devdump

import "strings"

// ddlToken reads one SQLite DDL token, skipping whitespace and SQL comments.
// Quoted strings/identifiers stay single tokens so their commas, parentheses
// and COLLATE text cannot be mistaken for column constraints.
func ddlToken(s string, pos int) (start, end int, text string, quoted bool) {
	for pos < len(s) {
		switch {
		case strings.ContainsRune(" \t\r\n\f\v", rune(s[pos])):
			pos++
		case strings.HasPrefix(s[pos:], "--"):
			if n := strings.IndexByte(s[pos:], '\n'); n >= 0 {
				pos += n + 1
			} else {
				pos = len(s)
			}
		case strings.HasPrefix(s[pos:], "/*"):
			if n := strings.Index(s[pos+2:], "*/"); n >= 0 {
				pos += n + 4
			} else {
				pos = len(s)
			}
		default:
			start = pos
			if strings.ContainsRune("\"'`[", rune(s[pos])) {
				closeQuote := s[pos]
				if closeQuote == '[' {
					closeQuote = ']'
				}
				pos++
				for pos < len(s) {
					if s[pos] == closeQuote {
						pos++
						if closeQuote != ']' && pos < len(s) && s[pos] == closeQuote {
							pos++
							continue
						}
						break
					}
					pos++
				}
				return start, pos, unquoteIdent(s[start:pos]), true
			}
			if strings.ContainsRune("(),]", rune(s[pos])) {
				return start, pos + 1, s[pos : pos+1], false
			}
			for pos < len(s) && !strings.ContainsRune(" \t\r\n\f\v(),\"'`[]", rune(s[pos])) &&
				!strings.HasPrefix(s[pos:], "--") && !strings.HasPrefix(s[pos:], "/*") {
				pos++
			}
			return start, pos, s[start:pos], false
		}
	}
	return pos, pos, "", false
}

func unquoteIdent(s string) string {
	if len(s) >= 2 {
		if s[0] == '[' && s[len(s)-1] == ']' {
			return s[1 : len(s)-1]
		}
		if strings.ContainsRune("\"'`", rune(s[0])) && s[len(s)-1] == s[0] {
			quote := s[:1]
			return strings.ReplaceAll(s[1:len(s)-1], quote+quote, quote)
		}
	}
	return s
}

// ddlBody extracts the CREATE TABLE body, ignoring quoted/comment delimiters.
func ddlBody(ddl string) string {
	open, depth := -1, 0
	for pos := 0; pos < len(ddl); {
		start, end, token, quoted := ddlToken(ddl, pos)
		pos = end
		if quoted {
			continue
		}
		switch token {
		case "(":
			if depth == 0 {
				open = end
			}
			depth++
		case ")":
			depth--
			if depth == 0 && open >= 0 {
				return ddl[open:start]
			}
		}
	}
	return ""
}

func splitColumnsDDL(body string) []string {
	var out []string
	start, depth := 0, 0
	for pos := 0; pos < len(body); {
		from, end, token, quoted := ddlToken(body, pos)
		pos = end
		if quoted {
			continue
		}
		switch token {
		case "(":
			depth++
		case ")":
			depth--
		case ",":
			if depth == 0 {
				out = append(out, body[start:from])
				start = end
			}
		}
	}
	return append(out, body[start:])
}

// Read only column-level COLLATE clauses. SQLite uses the last such clause;
// constraints inside expressions/table-level keys are handled independently.
func segmentColumnAndCollation(seg string) (name, coll string, hasColl bool) {
	_, pos, name, _ := ddlToken(seg, 0)
	depth := 0
	for pos < len(seg) {
		_, end, token, quoted := ddlToken(seg, pos)
		pos = end
		if quoted {
			continue
		}
		switch token {
		case "(":
			depth++
		case ")":
			depth--
		default:
			if depth == 0 && strings.EqualFold(token, "COLLATE") {
				_, pos, coll, _ = ddlToken(seg, pos)
				hasColl = coll != ""
			}
		}
	}
	return name, coll, hasColl
}
