package store

import (
	"errors"
	"log"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/taps/panel/internal/auth"
	"github.com/taps/panel/internal/config"
	"github.com/taps/panel/internal/model"
)

func Open(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(cfg.DBPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(model.All()...); err != nil {
		return nil, err
	}
	// Replace the legacy non-partial unique index on users.email with
	// a partial one. The old index was created by GORM's uniqueIndex
	// tag and treats every blank email as a collision; with multiple
	// password-only users that have no email, INSERTs/UPDATEs blow up
	// with "UNIQUE constraint failed: users.email". The partial index
	// only enforces uniqueness for non-empty emails — exactly what we
	// want for SSO matching.
	db.Exec(`DROP INDEX IF EXISTS idx_users_email`)
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_email_nonempty
	         ON users(email) WHERE email != ''`)
	// One-shot backfill, gated by a sentinel row in `settings`. Without
	// the gate this UPDATE re-ran on every panel start and silently
	// reset has_password=0 for any SSO-bound user who had since claimed
	// a real password — making "change password" degrade back to "set
	// password" (no current-password challenge) and effectively letting
	// any holder of a valid JWT take the account over permanently.
	const backfillKey = "migration.has_password_backfilled"
	var marker model.Setting
	if err := db.First(&marker, "key = ?", backfillKey).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		db.Exec(`UPDATE users SET has_password = 0
		         WHERE id IN (SELECT DISTINCT user_id FROM sso_identities)
		           AND has_password = 1`)
		db.Save(&model.Setting{Key: backfillKey, Value: "1"})
	}
	// One-shot CORS allowlist normalisation (audit N1). Strip trailing
	// "/" and lowercase the persisted CSV so the byte-equal comparison
	// in router.AllowOriginFunc matches what real browsers send. Bare
	// SQL keeps it transactional with the migration sentinel write —
	// gorm's Save would re-emit a struct with default fields the
	// settings table doesn't model.
	const corsNormKey = "migration.cors_origins_normalized"
	var corsMarker model.Setting
	if err := db.First(&corsMarker, "key = ?", corsNormKey).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		db.Exec(`UPDATE settings
		         SET value = lower(rtrim(value, '/'))
		         WHERE key = 'cors.allowedOrigins' AND value != ''`)
		db.Save(&model.Setting{Key: corsNormKey, Value: "1"})
	}
	// One-shot drop of the dead `captcha.config` JSON row (audit N6).
	// The captcha settings live as flat keys (captcha.provider /
	// captcha.siteKey / captcha.secret) since the Day 2 schema
	// rewrite; the old blob row is unused but lingered in older DBs.
	const captchaDropKey = "migration.captcha_legacy_dropped"
	var capMarker model.Setting
	if err := db.First(&capMarker, "key = ?", captchaDropKey).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		db.Exec(`DELETE FROM settings WHERE key = 'captcha.config'`)
		db.Save(&model.Setting{Key: captchaDropKey, Value: "1"})
	}
	// audit-2026-04-25 H3: lowercase username + email so SQLite's
	// case-sensitive default UNIQUE index matches the case-insensitive
	// matching enforced by the API layer. Refuse to migrate when the
	// dataset already contains case-only conflicts (e.g. both
	// "Foo@x.com" and "foo@x.com") — log the conflicting rows and
	// leave the sentinel unset so the operator can resolve it and
	// rerun. Once the data is clean, swap the byte-equal indexes for
	// LOWER()-based unique indexes so future writes can't reintroduce
	// case collisions.
	const lowerKey = "migration.users_email_username_lowercased"
	var lowerMarker model.Setting
	if err := db.First(&lowerMarker, "key = ?", lowerKey).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		if migrateLowerUsersIdent(db) {
			db.Save(&model.Setting{Key: lowerKey, Value: "1"})
		}
	}
	if err := seedAdmin(db, cfg); err != nil {
		return nil, err
	}
	return db, nil
}

func seedAdmin(db *gorm.DB, cfg *config.Config) error {
	var n int64
	db.Model(&model.User{}).Count(&n)
	if n > 0 {
		return nil
	}
	hash, err := auth.HashPassword(cfg.AdminPass)
	if err != nil {
		return err
	}
	u := &model.User{
		Username:           cfg.AdminUser,
		PasswordHash:       hash,
		Role:               model.RoleAdmin,
		MustChangePassword: true,
	}
	if err := db.Create(u).Error; err != nil && !errors.Is(err, gorm.ErrDuplicatedKey) {
		return err
	}
	log.Printf("seeded default admin user: %s / %s (must change on first login)", cfg.AdminUser, cfg.AdminPass)
	return nil
}

// migrateLowerUsersIdent rewrites users.username and users.email to
// lowercase + trimmed form, after first verifying no case-only
// collisions would violate the new LOWER()-based UNIQUE indexes.
// Returns true when the migration was applied (sentinel may be
// written by caller). Returns false on conflict so the sentinel
// is *not* written — admin resolves the conflict and the next
// boot retries.
func migrateLowerUsersIdent(db *gorm.DB) bool {
	type row struct {
		ID       uint
		Username string
		Email    string
	}
	var rows []row
	if err := db.Raw(`SELECT id, username, email FROM users`).Scan(&rows).Error; err != nil {
		log.Printf("[migrate-lower] read users: %v (skipping; will retry next boot)", err)
		return false
	}
	// Group by lowered key; any group with >1 ID is a collision.
	usernames := map[string][]uint{}
	emails := map[string][]uint{}
	for _, r := range rows {
		un := strings.ToLower(strings.TrimSpace(r.Username))
		if un != "" {
			usernames[un] = append(usernames[un], r.ID)
		}
		em := strings.ToLower(strings.TrimSpace(r.Email))
		if em != "" {
			emails[em] = append(emails[em], r.ID)
		}
	}
	conflict := false
	for k, ids := range usernames {
		if len(ids) > 1 {
			log.Printf("[migrate-lower] username collision: %q used by ids %v — resolve manually before this migration can run", k, ids)
			conflict = true
		}
	}
	for k, ids := range emails {
		if len(ids) > 1 {
			log.Printf("[migrate-lower] email collision: %q used by ids %v — resolve manually before this migration can run", k, ids)
			conflict = true
		}
	}
	if conflict {
		return false
	}
	// Apply in a single transaction so a mid-flight crash leaves the
	// rows untouched (sentinel is also unwritten → migration retries).
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE users SET username = lower(trim(username)) WHERE username != lower(trim(username))`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE users SET email = lower(trim(email)) WHERE email != '' AND email != lower(trim(email))`).Error; err != nil {
			return err
		}
		// Swap byte-equal unique indexes for LOWER()-based ones so
		// future inserts can't reintroduce case collisions even if a
		// caller bypasses the API layer's normalisation.
		if err := tx.Exec(`DROP INDEX IF EXISTS idx_users_username`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DROP INDEX IF EXISTS uniq_users_email_nonempty`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_username_lower ON users(lower(username))`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_email_lower_nonempty ON users(lower(email)) WHERE email != ''`).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Printf("[migrate-lower] apply: %v (sentinel not written; will retry next boot)", err)
		return false
	}
	log.Printf("[migrate-lower] users.username and users.email lowercased; LOWER() unique indexes installed")
	return true
}
