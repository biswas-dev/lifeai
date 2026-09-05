// Command admin is the operator CLI, shipped in the container so a locked-out
// account can be recovered without hand-written SQL:
//
//	admin users
//	admin reset-password <email> <new-password>
//	admin make-admin <email>
//	admin sync-75hard <email>
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/biswas-dev/lifeai/api/internal/auth"
	"github.com/biswas-dev/lifeai/api/internal/config"
	"github.com/biswas-dev/lifeai/api/internal/db"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cfg := config.Load()
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		fail("open database: %v", err)
	}
	defer database.Close()
	if _, err := database.Migrate(); err != nil {
		fail("migrate: %v", err)
	}

	switch os.Args[1] {
	case "users":
		rows, err := database.Query(`SELECT id, email, name, is_admin, auth_provider, created_at FROM users WHERE deleted_at IS NULL ORDER BY id`)
		if err != nil {
			fail("%v", err)
		}
		defer rows.Close()
		fmt.Printf("%-5s %-40s %-24s %-6s %-9s %s\n", "id", "email", "name", "admin", "provider", "created")
		for rows.Next() {
			var id int64
			var email, name, provider, created string
			var admin bool
			if err := rows.Scan(&id, &email, &name, &admin, &provider, &created); err != nil {
				fail("%v", err)
			}
			fmt.Printf("%-5d %-40s %-24s %-6v %-9s %s\n", id, email, name, admin, provider, created)
		}
	case "reset-password":
		if len(os.Args) != 4 {
			usage()
		}
		if len(os.Args[3]) < 8 {
			fail("password must be at least 8 characters")
		}
		hash, err := auth.HashPassword(os.Args[3])
		if err != nil {
			fail("%v", err)
		}
		res, err := database.Exec(`UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE lower(email) = ? AND deleted_at IS NULL`,
			hash, strings.ToLower(strings.TrimSpace(os.Args[2])))
		if err != nil {
			fail("%v", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			fail("no such user")
		}
		fmt.Println("password updated")
	case "make-admin":
		if len(os.Args) != 3 {
			usage()
		}
		res, err := database.Exec(`UPDATE users SET is_admin = 1 WHERE lower(email) = ? AND deleted_at IS NULL`, strings.ToLower(strings.TrimSpace(os.Args[2])))
		if err != nil {
			fail("%v", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			fail("no such user")
		}
		fmt.Println("user is now an admin")
	case "reset-2fa":
		if len(os.Args) != 3 {
			usage()
		}
		tx, err := database.Begin()
		if err != nil {
			fail("%v", err)
		}
		defer tx.Rollback()
		var userID int64
		if err = tx.QueryRow(`SELECT id FROM users WHERE lower(email)=? AND deleted_at IS NULL`, strings.ToLower(strings.TrimSpace(os.Args[2]))).Scan(&userID); err != nil {
			fail("no such user")
		}
		if _, err = tx.Exec(`UPDATE users SET totp_secret_enc='',totp_last_step=-1,mfa_failures=0,mfa_locked_until=0,security_version=security_version+1 WHERE id=?`, userID); err != nil {
			fail("%v", err)
		}
		if _, err = tx.Exec(`DELETE FROM recovery_codes WHERE user_id=?`, userID); err != nil {
			fail("%v", err)
		}
		if _, err = tx.Exec(`DELETE FROM auth_challenges WHERE user_id=?`, userID); err != nil {
			fail("%v", err)
		}
		if err = tx.Commit(); err != nil {
			fail("%v", err)
		}
		fmt.Println("authenticator 2FA reset; browser sessions revoked; passkeys retained")
	case "reset-75hard":
		// Clears the last-sync marker so the next scheduler tick pulls again.
		if len(os.Args) != 3 {
			usage()
		}
		_, err := database.Exec(`UPDATE integrations SET last_sync_at = NULL WHERE provider = '75hard' AND user_id = (SELECT id FROM users WHERE lower(email) = ?)`,
			strings.ToLower(strings.TrimSpace(os.Args[2])))
		if err != nil {
			fail("%v", err)
		}
		fmt.Println("next scheduler tick will pull from 75hard")
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: admin users | reset-password <email> <password> | reset-2fa <email> | make-admin <email> | reset-75hard <email>")
	os.Exit(2)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
