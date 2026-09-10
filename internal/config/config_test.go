package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddAndLoadUnixTarget(t *testing.T) {
	dir := t.TempDir()
	target := Target{Name: "production", Network: "unix", Address: "/run/mysqld/mysqld.sock", User: "monitor"}
	if err := AddTarget(dir, target, []byte("secret\n")); err != nil {
		t.Fatal(err)
	}
	got, password, err := LoadTarget(dir, "production")
	if err != nil {
		t.Fatal(err)
	}
	if got.Address != target.Address || string(password) != "secret" {
		t.Fatalf("unexpected target: %#v %q", got, password)
	}
	info, err := os.Stat(filepath.Join(dir, "secrets", "production.password"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("secret mode is %o", info.Mode().Perm())
	}
}

func TestTCPRequiresVerifiedTLS(t *testing.T) {
	target := Target{Name: "db", Network: "tcp", Address: "10.0.0.1:3306", User: "monitor"}
	if err := ValidateTarget(target); err == nil {
		t.Fatal("insecure TCP target was accepted")
	}
	target.TLSCAFile = "/etc/ca.pem"
	target.TLSServerName = "db.internal"
	if err := ValidateTarget(target); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsUnsafeTargetName(t *testing.T) {
	if err := ValidateTarget(Target{Name: "../secret", Network: "unix", Address: "/tmp/mysql.sock", User: "u"}); err == nil {
		t.Fatal("unsafe target name was accepted")
	}
}
