package patches

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// A stretch of code is matched as that code, however a release names what it names and wherever it breaks its lines,
// and as nothing else.
func TestShapeMatchesTheCodeAndNothingElse(t *testing.T) {
	const code = `var «a»=«f»({x:!1,y:"a b"}),«b»=«a»?.z??[];return «b»===void 0?«a»:(«c»=>«c».y)(...«b»)`
	var b shapeBuilder
	b.code(code)
	re := regexp.MustCompile(`^(?:` + b.String() + `)$`)
	if want := map[string][]int{"a": {1, 4, 6}, "f": {2}, "b": {3, 5, 9}, "c": {7, 8}}; !reflect.DeepEqual(b.names, want) {
		t.Fatalf("the roles are written as groups %v, want %v", b.names, want)
	}

	const written = `var m=k1({x:!1,y:"a b"}),$n=m?.z??[];return $n===void 0?m:(p_=>p_.y)(...$n)`
	got := re.FindStringSubmatch(written)
	if want := []string{written, "m", "k1", "$n", "m", "$n", "m", "p_", "p_", "$n"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the names are %q, want %q", got, want)
	}
	// With a line break wherever the minifier could put one, and with white space between any two tokens.
	positions := lineBreakPositions(written)
	if len(positions) < 10 {
		t.Fatalf("only %d places for a line break", len(positions))
	}
	for _, pos := range positions {
		if broken := written[:pos] + "\n" + written[pos:]; !re.MatchString(broken) {
			t.Errorf("a line break at offset %d is not matched: %q", pos, broken)
		}
	}
	if spaced := `var  m = k1 ( { x : ! 1 , y : "a b" } ) , $n = m ?. z ?? [ ] ; return  $n === void  0 ? m : ( p_ => p_ . y ) ( ... $n )`; !re.MatchString(spaced) {
		t.Error("white space between the tokens is not matched")
	}

	for name, other := range map[string]string{
		"a string with other text":       strings.Replace(written, `"a b"`, `"a  b"`, 1),
		"a string with other quotes":     strings.Replace(written, `"a b"`, `'a b'`, 1),
		"one more property":              strings.Replace(written, `y:"a b"`, `y:"a b",z:1`, 1),
		"one property fewer":             strings.Replace(written, `x:!1,`, ``, 1),
		"a property by another name":     strings.Replace(written, `m?.z`, `m?.w`, 1),
		"another operator":               strings.Replace(written, `===`, `!==`, 1),
		"one more statement":             strings.Replace(written, `;return`, `;q();return`, 1),
		"something before it":            "q();" + written,
		"something after it":             written + ";q()",
		"two words run together":         strings.Replace(written, `var m`, `varm`, 1),
		"a word and a number together":   strings.Replace(written, `void 0`, `void0`, 1),
		"white space inside an operator": strings.Replace(written, `m?.z`, `m? .z`, 1),
		"white space inside an arrow":    strings.Replace(written, `p_=>`, `p_= >`, 1),
		"white space inside a spread":    strings.Replace(written, `...$n`, `.. .$n`, 1),
		"white space inside a string":    strings.Replace(written, `"a b"`, "\"a\nb\"", 1),
		"a name that is no name":         strings.Replace(written, `k1(`, `k-1(`, 1),
		"a name left out":                strings.Replace(written, `var m=`, `var =`, 1),
		"a word that is not the word":    strings.Replace(written, `return`, `returns`, 1),
		"a keyword in other letters":     strings.Replace(written, `void`, `VOID`, 1),
	} {
		if other == written {
			t.Fatalf("%s: nothing was changed", name)
		}
		if re.MatchString(other) {
			t.Errorf("%s is matched: %q", name, other)
		}
	}

	// A role is any name in each place it is written, which is why sameNames has to look.
	groups := func(src string) [][]byte {
		loc := re.FindStringSubmatchIndex(src)
		if loc == nil {
			t.Fatalf("%q is not matched", src)
		}
		return submatches([]byte(src), loc)
	}
	if !sameNames(groups(written), b.names) {
		t.Error("the same name in every place of a role is not taken for one name")
	}
	for _, other := range []string{
		strings.Replace(written, `=m?.z`, `=q?.z`, 1),
		strings.Replace(written, `0?m:`, `0?q:`, 1),
		strings.Replace(written, `p_.y`, `p.y`, 1),
		strings.Replace(written, `...$n`, `...$m`, 1),
		strings.Replace(written, `return $n`, `return n`, 1),
	} {
		if sameNames(groups(other), b.names) {
			t.Errorf("two names for one role are taken for one name in %q", other)
		}
	}
}

// An operator of several characters is matched as that operator, whole.
func TestShapeKeepsAnOperatorWhole(t *testing.T) {
	for _, op := range []string{
		"...", "=>", "?.", "??", "&&", "||", "==", "!=", "===", "!==", "<=", ">=", "++", "--", "**", "<<", ">>", ">>>",
		"+=", "-=", "*=", "/=", "%=", "**=", "<<=", ">>=", ">>>=", "&=", "|=", "^=", "&&=", "||=", "??=",
	} {
		var b shapeBuilder
		b.code("«a»" + op + "«b»")
		re := regexp.MustCompile(`^(?:` + b.String() + `)$`)
		if !re.MatchString("x"+op+"y") || !re.MatchString("x "+op+"\ny") {
			t.Errorf("%s is not matched between two names", op)
		}
		for i := 1; i < len(op); i++ {
			if split := "x" + op[:i] + " " + op[i:] + "y"; re.MatchString(split) {
				t.Errorf("%s is matched as %q", op, split)
			}
		}
	}
}

// Stretches of code go into one pattern with groups and pieces of pattern around them, and a role is told by the
// number its groups have in the whole.
func TestShapesGoIntoOnePattern(t *testing.T) {
	var b shapeBuilder
	whole := b.open()
	b.code(`«a»=«f»();`)
	gap := b.open()
	b.raw(`[\s\S]{0,20}?`)
	b.close()
	b.close()
	rest := b.open()
	b.raw(`\s*`)
	b.code(`return «a»`)
	b.close()
	if whole != 1 || gap != 4 || rest != 5 {
		t.Fatalf("the groups are %d, %d and %d", whole, gap, rest)
	}
	if want := map[string][]int{"a": {2, 6}, "f": {3}}; !reflect.DeepEqual(b.names, want) {
		t.Fatalf("the roles are written as groups %v, want %v", b.names, want)
	}
	re := regexp.MustCompile(b.String())
	got := re.FindStringSubmatch("var x=y();x.z=1;\nreturn x}")
	if want := []string{"x=y();x.z=1;\nreturn x", "x=y();x.z=1;", "x", "y", "x.z=1;", "\nreturn x", "x"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("the groups are %q, want %q", got, want)
	}
	// A string is its text to the letter, up to the quote that ends it.
	var q shapeBuilder
	q.code(`«a»("x\"y,z",'it\'s')`)
	if quoted := regexp.MustCompile(`^(?:` + q.String() + `)$`); !quoted.MatchString(`f("x\"y,z",'it\'s')`) || quoted.MatchString(`f("x\"y, z",'it\'s')`) || quoted.MatchString(`f("x\"y,z",'it\' s')`) {
		t.Errorf("a string with a quote in it is matched by %q", q.String())
	}
	// Nothing joins two stretches but what is put between them.
	var c shapeBuilder
	c.code(`a`)
	c.code(`b`)
	if c.String() != "ab" {
		t.Errorf("two stretches are joined as %q", c.String())
	}

	for _, code := range []string{`«a»=«b`, `a="b`, `a='b"`} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q was taken for code", code)
				}
			}()
			var b shapeBuilder
			b.code(code)
		}()
	}
}

func TestMentions(t *testing.T) {
	for _, c := range []struct {
		code, name string
		want       bool
	}{
		{"g", "g", true},
		{"f&&b.current!==f&&(b.current=f,a(Kp(!1)))", "f", true},
		{"f&&b.current!==f&&(b.current=f,a(Kp(!1)))", "Kp", true},
		{"f&&b.current!==f&&(b.current=f,a(Kp(!1)))", "current", false},
		{"f&&b.current!==f&&(b.current=f,a(Kp(!1)))", "K", false},
		{"f&&b.current!==f&&(b.current=f,a(Kp(!1)))", "p", false},
		{"h=(0,B.useRef)(null);lFc();", "l", false},
		{"h=(0,B.useRef)(null);lFc();", "B", true},
		{"h=(0,B.useRef)(null);lFc();", "useRef", false},
		{"x.g=1", "g", false},
		{"x?.g", "g", false},
		{"x.g;g=1", "g", true},
		{"gg;g$;$g;_g;g1;1g", "g", false},
		{"var{a:x,...g}=y", "g", true},
		{"f(...g)", "g", true},
		{"[..g]", "g", false},
		{"var g", "g", true},
		{"{g}", "g", true},
		{"a\ng\nb", "g", true},
		{"$w=1", "$w", true},
		{"a$w=1", "$w", false},
		{"", "g", false},
		{"g", "", false},
	} {
		if got := mentions([]byte(c.code), c.name); got != c.want {
			t.Errorf("mentions(%q, %q) = %v, want %v", c.code, c.name, got, c.want)
		}
	}
}
