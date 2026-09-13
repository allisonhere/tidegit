package diff

import (
	"path/filepath"
	"strings"
	"unicode"
)

// TokenClass is a syntax-highlighting category. The viewer maps it to a theme
// colour; the parser never chooses colours.
type TokenClass uint8

const (
	TokPlain TokenClass = iota
	TokKeyword
	TokString
	TokComment
	TokNumber
)

// Token is a highlighted span within a line, in rune offsets.
type Token struct {
	Start, End int
	Class      TokenClass
}

type langSpec struct {
	lineComment string
	keywords    map[string]bool
}

var keywords = map[string]map[string]bool{
	"go":     set("break default func interface select case defer go map struct chan else goto package switch const fallthrough if range type continue for import return var"),
	"rust":   set("as break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while async await"),
	"python": set("and as assert async await break class continue def del elif else except False finally for from global if import in is lambda None nonlocal not or pass raise return True try while with yield"),
	"js":     set("await break case catch class const continue debugger default delete do else export extends finally for function if import in instanceof let new return static super switch this throw try typeof var void while with yield true false null undefined"),
	"json":   set("true false null"),
	"c":      set("auto break case char const continue default do double else enum extern float for goto if inline int long register return short signed sizeof static struct switch typedef union unsigned void volatile while"),
	"shell":  set("if then else elif fi for while do done case esac function in return local export readonly true false break continue"),
	"toml":   set("true false"),
	"yaml":   set("true false null yes no"),
	"sql":    set("select from where insert update delete join left right inner outer group by order having limit offset values into set create table alter drop index and or not null true false"),
}

func set(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

// LanguageForPath picks a language from a file extension. An unknown extension
// returns "", which highlights as plain text.
func LanguageForPath(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "go.mod", "go.sum":
		return "go"
	case "makefile", "dockerfile":
		return "shell"
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".rs":
		return "rust"
	case ".py", ".pyi":
		return "python"
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx":
		return "js"
	case ".json":
		return "json"
	case ".c", ".h", ".cc", ".cpp", ".hpp", ".cxx", ".java", ".cs", ".kt":
		return "c"
	case ".sh", ".bash", ".zsh", ".fish":
		return "shell"
	case ".toml":
		return "toml"
	case ".yaml", ".yml":
		return "yaml"
	case ".sql":
		return "sql"
	default:
		return ""
	}
}

// Highlight tokenizes one line for a language. It is single-line by design:
// block comments spanning lines are not carried across, which is the right
// trade-off for a diff viewport that renders lines independently. An empty
// language yields no tokens.
func Highlight(lang, text string) []Token {
	if lang == "" || text == "" {
		return nil
	}
	kw := keywords[lang]
	lineComment := lineCommentFor(lang)
	runes := []rune(text)
	var out []Token
	i := 0
	for i < len(runes) {
		r := runes[i]
		if lineComment != "" && strings.HasPrefix(string(runes[i:]), lineComment) {
			out = append(out, Token{Start: i, End: len(runes), Class: TokComment})
			break
		}
		if r == '"' || r == '\'' || r == '`' {
			j := i + 1
			for j < len(runes) {
				if runes[j] == '\\' && j+1 < len(runes) {
					j += 2
					continue
				}
				if runes[j] == r {
					j++
					break
				}
				j++
			}
			out = append(out, Token{Start: i, End: j, Class: TokString})
			i = j
			continue
		}
		if unicode.IsDigit(r) {
			j := i
			for j < len(runes) && (unicode.IsDigit(runes[j]) || strings.ContainsRune("abcdefABCDEFxXoO._", runes[j])) {
				j++
			}
			out = append(out, Token{Start: i, End: j, Class: TokNumber})
			i = j
			continue
		}
		if unicode.IsLetter(r) || r == '_' {
			j := i
			for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) || runes[j] == '_') {
				j++
			}
			word := string(runes[i:j])
			if kw[word] {
				out = append(out, Token{Start: i, End: j, Class: TokKeyword})
			}
			i = j
			continue
		}
		i++
	}
	return out
}

func lineCommentFor(lang string) string {
	switch lang {
	case "python", "shell", "toml", "yaml":
		return "#"
	case "sql":
		return "--"
	case "go", "rust", "js", "json", "c":
		return "//"
	default:
		return ""
	}
}
