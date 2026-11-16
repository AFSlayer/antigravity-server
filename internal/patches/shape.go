package patches

import (
	"bytes"
	"regexp"
	"strings"
)

// A shapeBuilder writes a pattern out of stretches of minified code, each matched as that code and nothing else, and
// of pieces of pattern put in by hand around them.
//
// Code is matched token for token, with any white space between two tokens, since a release breaks the bundle's lines
// where it likes. A name a release may change is written in the code as «role», and is a group in the pattern. names
// gives, for each role, the groups it is written as: where a role is written more than once, the pattern cannot say
// that the same name has to stand in every place, so whoever uses it has to look, with sameNames.
//
// A pattern with gaps in it matches code that has changed in the gaps. This is for a patch that copies or moves what
// it matches, where changed code has to be reported as not found rather than carried along.
type shapeBuilder struct {
	pattern strings.Builder
	groups  int
	names   map[string][]int
}

// open starts a group and gives its number.
func (b *shapeBuilder) open() int {
	b.groups++
	b.pattern.WriteString("(")
	return b.groups
}

// close ends the group that was opened last.
func (b *shapeBuilder) close() { b.pattern.WriteString(")") }

// raw adds a piece of pattern as it is. It must have no group of its own in it, or the numbers would be off.
func (b *shapeBuilder) raw(pattern string) { b.pattern.WriteString(pattern) }

// code adds the shape of a stretch of code. What joins it to the pattern before it is for the caller to say.
func (b *shapeBuilder) code(code string) {
	if b.names == nil {
		b.names = map[string][]int{}
	}
	first, prevWord := true, false
	for i := 0; i < len(code); {
		var piece string
		word := false
		switch c := code[i]; {
		case c == ' ' || c == '\n' || c == '\t':
			i++
			continue
		case strings.HasPrefix(code[i:], "«"):
			end := strings.Index(code[i:], "»")
			if end < 0 {
				panic("patches: a role without its end in " + code[i:])
			}
			role := code[i+len("«") : i+end]
			i += end + len("»")
			b.groups++
			b.names[role] = append(b.names[role], b.groups)
			piece, word = `([a-zA-Z0-9_$]+)`, true
		case c == '"' || c == '\'':
			end := i + 1
			for end < len(code) && code[end] != c {
				if code[end] == '\\' {
					end++
				}
				end++
			}
			if end >= len(code) {
				panic("patches: a string without its end in " + code[i:])
			}
			piece = regexp.QuoteMeta(code[i : end+1])
			i = end + 1
		case isJSIdentByte(c):
			end := i
			for end < len(code) && isJSIdentByte(code[end]) {
				end++
			}
			piece, word = regexp.QuoteMeta(code[i:end]), true
			i = end
		default:
			// An operator of several characters is one token: white space inside it would make it something else.
			op := code[i : i+1]
			for _, long := range jsLongOperators {
				if strings.HasPrefix(code[i:], long) {
					op = long
					break
				}
			}
			piece = regexp.QuoteMeta(op)
			i += len(op)
		}
		switch {
		case first:
		case prevWord && word:
			// Two words in a row are two words only with white space between them.
			b.pattern.WriteString(`\s+`)
		default:
			b.pattern.WriteString(`\s*`)
		}
		b.pattern.WriteString(piece)
		first, prevWord = false, word
	}
}

func (b *shapeBuilder) String() string { return b.pattern.String() }

// JavaScript's operators of more than one character, each before any that it starts with.
var jsLongOperators = []string{
	">>>=", "...", "===", "!==", "**=", "<<=", ">>=", ">>>", "&&=", "||=", "??=",
	"=>", "==", "!=", "<=", ">=", "&&", "||", "??", "?.", "++", "--", "**", "<<", ">>", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=",
}

// sameNames says whether every role stands for one name in what a pattern captured.
func sameNames(groups [][]byte, names map[string][]int) bool {
	for _, at := range names {
		for _, g := range at[1:] {
			if !bytes.Equal(groups[g], groups[at[0]]) {
				return false
			}
		}
	}
	return true
}

// mentions says whether a stretch of code has a name in it as a word of its own. A property read off something is not
// that name, and what a rest or a spread is of is.
func mentions(code []byte, name string) bool {
	for from := 0; name != ""; {
		at := bytes.Index(code[from:], []byte(name))
		if at < 0 {
			break
		}
		start, end := from+at, from+at+len(name)
		from = start + 1
		if end < len(code) && isJSIdentByte(code[end]) {
			continue
		}
		if start > 0 && isJSIdentByte(code[start-1]) {
			continue
		}
		if start > 0 && code[start-1] == '.' && !bytes.HasSuffix(code[:start], []byte("...")) {
			continue
		}
		return true
	}
	return false
}
