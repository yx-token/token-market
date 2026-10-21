package compat

import (
	"testing"
)

func TestSplitStatementsHandlesQuotesCommentsAndDollarBodies(t *testing.T) {
	src := `-- leading comment; not a statement
ALTER TABLE accounts ADD COLUMN note text DEFAULT 'a;b''c';
/* block ; /* nested ; */ still comment */
DO $$
BEGIN
  EXECUTE 'ALTER TABLE x DROP COLUMN y';
END
$$;
CREATE FUNCTION f() RETURNS trigger AS $fn$ BEGIN RETURN NEW; END $fn$ LANGUAGE plpgsql;
UPDATE "Weird;Name" SET a = E'it\'s;fine';
SELECT $1;`
	stmts := SplitStatements(src)
	if len(stmts) != 5 {
		for _, s := range stmts {
			t.Logf("line %d: %q", s.Line, s.Text)
		}
		t.Fatalf("got %d statements, want 5", len(stmts))
	}
	if stmts[0].Line != 2 {
		t.Errorf("first statement line = %d, want 2", stmts[0].Line)
	}
	if stmts[1].Line != 4 {
		t.Errorf("DO block line = %d, want 4", stmts[1].Line)
	}
	if stmts[4].Text != "SELECT $1" {
		t.Errorf("positional parameter mangled: %q", stmts[4].Text)
	}
}

func TestTokenizeStringsAndIdentifiers(t *testing.T) {
	toks := tokenize(`ALTER TABLE "Accounts" ADD CONSTRAINT c CHECK (platform IN ('a''b', E'c\'d'))`)
	var strs []string
	for _, tk := range toks {
		if tk.kind == tokString {
			strs = append(strs, tk.text)
		}
	}
	if len(strs) != 2 || strs[0] != "a'b" || strs[1] != "c'd" {
		t.Fatalf("strings = %q", strs)
	}
	if toks[2].kind != tokQuoted || toks[2].text != "Accounts" {
		t.Fatalf("quoted identifier = %+v", toks[2])
	}
}
