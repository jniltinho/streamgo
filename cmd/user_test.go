package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jniltinho/streamgo/internal/core/account"
	"github.com/jniltinho/streamgo/internal/store"
	"github.com/jniltinho/streamgo/internal/store/storetest"
)

// executeIn runs the CLI in-process with stdin.
func executeIn(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// userEnv prepares a migrated database of the driver and returns the accounts service on it,
// as the server would use it.
func userEnv(t *testing.T, driver string) *account.Service {
	t.Helper()
	dataDir := validEnv(t)
	s := store.Settings{Driver: store.SQLite, SQLitePath: filepath.Join(dataDir, "streamgo.db")}
	if driver != store.SQLite {
		s = storetest.NewSettings(t, driver)
		t.Setenv("STREAMGO_DATABASE_DRIVER", driver)
		t.Setenv("STREAMGO_DATABASE_DSN", s.DSN)
	}
	t.Chdir(t.TempDir())
	if code, _, errOut := executeIn(t, "", "migrate", "up"); code != 0 {
		t.Fatalf("migrate: %s", errOut)
	}
	return account.New(store.NewAccounts(storetest.OpenEmpty(t, s)), account.Options{})
}

func listUsers(t *testing.T) []userJSON {
	t.Helper()
	code, out, errOut := executeIn(t, "", "user", "list", "--json")
	var users []userJSON
	if code != 0 || json.Unmarshal([]byte(out), &users) != nil {
		t.Fatalf("list: exit %d %q %q", code, out, errOut)
	}
	return users
}

func TestUserCommand(t *testing.T) {
	for _, driver := range storetest.Drivers() {
		t.Run(driver, func(t *testing.T) { userCommand(t, driver) })
	}
}

func userCommand(t *testing.T, driver string) {
	ctx := context.Background()
	accounts := userEnv(t, driver)

	// Create a user: the user exists and can sign in with the password from stdin
	code, out, errOut := executeIn(t, "correct-horse\n", "user", "add", "--email", "ana@example.com", "--name", "Ana Souza", "--password-stdin")
	if code != 0 || !strings.Contains(out, "ana@example.com") {
		t.Fatalf("add: exit %d %q %q", code, out, errOut)
	}
	_, token, err := accounts.Login(ctx, "ana@example.com", "correct-horse")
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}

	// Duplicate e-mail in another letter case, and a short password: exit 1, nothing stored
	if code, _, errOut := executeIn(t, "another-password\n", "user", "add", "--email", "ANA@Example.com", "--name", "Other", "--password-stdin"); code != 1 || !strings.Contains(errOut, "already exists") {
		t.Errorf("duplicate: exit %d %q", code, errOut)
	}
	if code, _, errOut := executeIn(t, "123456789\n", "user", "add", "--email", "bob@example.com", "--name", "Bob", "--password-stdin"); code != 1 || !strings.Contains(errOut, "password") {
		t.Errorf("short password: exit %d %q", code, errOut)
	}
	if users := listUsers(t); len(users) != 1 || users[0].Email != "ana@example.com" || users[0].Status != "active" {
		t.Fatalf("users: %+v", users)
	}

	// never from a flag, and no prompt without a terminal
	if code, _, _ := executeIn(t, "", "user", "add", "--email", "bob@example.com", "--name", "Bob", "--password", "0123456789"); code != 1 {
		t.Errorf("--password accepted")
	}
	if code, _, errOut := executeIn(t, "", "user", "add", "--email", "bob@example.com", "--name", "Bob"); code != 1 || !strings.Contains(errOut, "--password-stdin") {
		t.Errorf("no terminal: exit %d %q", code, errOut)
	}

	// Disabled while signed in: the session stops at once
	if code, _, errOut := executeIn(t, "", "user", "disable", "ana@example.com"); code != 0 {
		t.Fatalf("disable: %q", errOut)
	}
	if _, err := accounts.Authenticate(ctx, token); !errors.Is(err, account.ErrUnauthenticated) {
		t.Fatalf("session after disable: %v", err)
	}
	if users := listUsers(t); !strings.HasPrefix(users[0].Status, "disabled") {
		t.Fatalf("status: %q", users[0].Status)
	}
	if code, _, errOut := executeIn(t, "", "user", "enable", "ana@example.com"); code != 0 {
		t.Fatalf("enable: %q", errOut)
	}

	// passwd changes the password
	if code, _, errOut := executeIn(t, "battery-staple\n", "user", "passwd", "ana@example.com", "--password-stdin"); code != 0 {
		t.Fatalf("passwd: %q", errOut)
	}
	if _, _, err := accounts.Login(ctx, "ana@example.com", "battery-staple"); err != nil {
		t.Fatalf("sign in with the new password: %v", err)
	}
	if code, _, _ := executeIn(t, "", "user", "disable", "nobody@example.com"); code != 1 {
		t.Error("disabling an unknown user succeeded")
	}

	// First account: ana is an administrator, the next account added the same way is not,
	// one added with --admin is
	if code, _, errOut := executeIn(t, "bruno-password\n", "user", "add", "--email", "bruno@example.com", "--name", "Bruno", "--password-stdin"); code != 0 {
		t.Fatalf("add bruno: %q", errOut)
	}
	if code, out, errOut := executeIn(t, "carla-password\n", "user", "add", "--email", "carla@example.com", "--name", "Carla", "--admin", "--password-stdin"); code != 0 || !strings.Contains(out, "administrator") {
		t.Fatalf("add carla --admin: %q %q", out, errOut)
	}
	roles := func() string {
		t.Helper()
		var parts []string
		for _, u := range listUsers(t) {
			parts = append(parts, u.Email+"="+u.Role)
		}
		return strings.Join(parts, " ")
	}
	if got, want := roles(), "ana@example.com=administrator bruno@example.com=user carla@example.com=administrator"; got != want {
		t.Fatalf("roles %q, want %q", got, want)
	}

	// Grant and revoke from the CLI; the text list shows the role
	if code, _, errOut := executeIn(t, "", "user", "admin", "bruno@example.com"); code != 0 {
		t.Fatalf("admin: %q", errOut)
	}
	if got := roles(); !strings.Contains(got, "bruno@example.com=administrator") {
		t.Fatalf("after admin: %q", got)
	}
	if code, out, errOut := executeIn(t, "", "user", "list"); code != 0 || !strings.Contains(out, "ROLE") || !strings.Contains(out, "administrator") {
		t.Fatalf("text list: %q %q", out, errOut)
	}
	if code, _, errOut := executeIn(t, "", "user", "admin", "BRUNO@example.com", "--revoke"); code != 0 {
		t.Fatalf("admin --revoke: %q", errOut)
	}
	if got := roles(); !strings.Contains(got, "bruno@example.com=user") {
		t.Fatalf("after --revoke: %q", got)
	}
	if code, _, _ := executeIn(t, "", "user", "admin", "nobody@example.com"); code != 1 {
		t.Error("granting an unknown user succeeded")
	}
}
