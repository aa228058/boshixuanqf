package account

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSessionNamesForLogin(t *testing.T) {
	accountA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	accountB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	formalA, temporaryA := sessionNamesForLogin("", accountA)
	formalB, temporaryB := sessionNamesForLogin("", accountB)
	if formalA != "acc_"+accountA.String() || formalB != "acc_"+accountB.String() {
		t.Fatalf("unexpected generated formal names: %q, %q", formalA, formalB)
	}
	if !strings.HasPrefix(temporaryA, formalA+"_login_") || !strings.HasPrefix(temporaryB, formalB+"_login_") {
		t.Fatalf("temporary names must be scoped to their formal account names: %q, %q", temporaryA, temporaryB)
	}
	if temporaryA == temporaryB {
		t.Fatal("different accounts must never share a temporary session name")
	}

	formal, temporary := sessionNamesForLogin("  imported-session  ", accountA)
	if formal != "imported-session" || !strings.HasPrefix(temporary, formal+"_login_") {
		t.Fatalf("unexpected names for imported session: formal=%q temporary=%q", formal, temporary)
	}
	_, secondTemporary := sessionNamesForLogin("imported-session", accountA)
	if temporary == secondTemporary {
		t.Fatal("concurrent login attempts must receive distinct temporary session names")
	}
}
