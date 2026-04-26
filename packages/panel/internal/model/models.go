package model

import "time"

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
	RoleGuest Role = "guest"
)

type User struct {
	ID                  uint      `gorm:"primaryKey" json:"id"`
	Username            string    `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash        string    `gorm:"not null" json:"-"`
	Role                Role      `gorm:"size:16;not null;default:user" json:"role"`
	MustChangePassword  bool      `gorm:"default:false" json:"mustChangePassword"`
	// HasPassword distinguishes accounts with a password the user
	// actually knows from accounts whose PasswordHash is just a random
	// placeholder (SSO auto-created users). Without this flag, the
	// change-password endpoint would refuse those users (no "current
	// password" to provide). Default true so existing rows keep
	// behaving normally; SSO auto-create flips it false, and the
	// change-password endpoint flips it true once the user picks one.
	HasPassword         bool      `gorm:"default:true" json:"hasPassword"`
	// Email is the canonical identity for OIDC matching. Optional for
	// purely-local accounts; uniqueness is enforced by a partial index
	// in store.go (WHERE email != '') so multiple legacy rows with
	// blank emails don't collide. Admin-only edit (regular users see
	// it but can't change it — preventing self-service email changes
	// that would silently re-route SSO logins).
	Email               string    `gorm:"size:255;index" json:"email,omitempty"`
	// TokensInvalidBefore is the cutoff used to mass-invalidate any
	// JWT issued for this user. Bumped (set to time.Now()) on:
	// password change, role change, or admin-initiated overwrite.
	// Auth middleware refuses any token whose `iat` is older than
	// this value — without needing a per-jti revocation table.
	// Zero value (default) means "no revocation, all tokens valid".
	TokensInvalidBefore time.Time `gorm:"index" json:"-"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type Daemon struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:64;not null" json:"name"`
	Address   string    `gorm:"size:255;not null" json:"address"` // host:port
	Token     string    `gorm:"not null" json:"-"`
	LastSeen  time.Time `json:"lastSeen"`
	CreatedAt time.Time `json:"createdAt"`

	// DisplayHost is what we show to players in connection strings (e.g. the
	// public domain or NATed IP), separate from Address which is how the
	// panel reaches the daemon. Empty → fall back to Address host.
	DisplayHost string `gorm:"size:255" json:"displayHost"`
	// Port allocation range used by the auto free-port helper. 0 means
	// unset → defaults are 25565..25600.
	PortMin int `json:"portMin"`
	PortMax int `json:"portMax"`

	// CertFingerprint pins the daemon's self-signed TLS certificate
	// (SHA-256 in colon-hex form, e.g. "ab:cd:..."). Set on first
	// connection (TOFU) via the add-daemon wizard, enforced by the
	// daemonclient TLS dialer thereafter. An empty value means "not
	// yet pinned" — the dialer will refuse to connect until it is.
	CertFingerprint string `gorm:"size:128" json:"certFingerprint,omitempty"`
}

// Permission bits used by InstancePermission.Perms. RoleAdmin bypasses all
// these checks. For non-admin users a missing permission row means PermNone.
const (
	PermView     uint32 = 1 << 0 // see in lists / view config
	PermControl  uint32 = 1 << 1 // start / stop / kill / send input
	PermFiles    uint32 = 1 << 2 // browse + edit working dir files / backups
	PermTerminal uint32 = 1 << 3 // open the WebSocket terminal
	PermManage   uint32 = 1 << 4 // edit instance config (rare; admin-equivalent)
	PermAll             = PermView | PermControl | PermFiles | PermTerminal | PermManage
)

// InstancePermission grants access to a specific (daemon, instance) for a user.
// We don't keep instances in panel DB — they live on the daemon — so the key
// is the (daemonId, uuid) pair.
type InstancePermission struct {
	UserID   uint   `gorm:"primaryKey" json:"userId"`
	DaemonID uint   `gorm:"primaryKey" json:"daemonId"`
	UUID     string `gorm:"primaryKey;size:36" json:"uuid"`
	Perms    uint32 `json:"perms"` // bitmask of Perm* above
}

type TaskAction string

const (
	TaskCommand TaskAction = "command"
	TaskStart   TaskAction = "start"
	TaskStop    TaskAction = "stop"
	TaskRestart TaskAction = "restart"
	TaskBackup  TaskAction = "backup"
)

type Task struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	DaemonID  uint       `gorm:"index;not null" json:"daemonId"`
	UUID      string     `gorm:"index;size:36;not null" json:"uuid"`
	Name      string     `gorm:"size:128" json:"name"`
	Cron      string     `gorm:"size:64;not null" json:"cron"`
	Action    TaskAction `gorm:"size:16;not null" json:"action"`
	Data      string     `gorm:"type:text" json:"data"` // for action=command, the line to send
	Enabled   bool       `gorm:"default:true" json:"enabled"`
	LastRun   time.Time  `json:"lastRun"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

type APIKey struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	UserID       uint       `gorm:"index;not null" json:"userId"`
	Name         string     `gorm:"size:64" json:"name"`
	KeyHash      string     `gorm:"uniqueIndex;not null" json:"-"`
	Prefix       string     `gorm:"size:16;index" json:"prefix"` // first 8 chars for display
	IPWhitelist  string     `gorm:"size:255" json:"ipWhitelist"` // CSV; empty = any
	Scopes       string     `gorm:"size:255" json:"scopes"`      // CSV; empty = full
	LastUsed     time.Time  `json:"lastUsed"`
	CreatedAt    time.Time  `json:"createdAt"`
	// ExpiresAt is the optional auto-expiration. Nil = never expires.
	// LookupAPIKey rejects any key whose ExpiresAt is in the past.
	ExpiresAt    *time.Time `gorm:"index" json:"expiresAt,omitempty"`
	// RevokedAt is set by manual revocation (per-key DELETE-equivalent
	// or "revoke all" sweep). Once set, LookupAPIKey rejects the row.
	// Kept (not deleted) so the audit trail / last-used timestamp
	// stays queryable.
	RevokedAt    *time.Time `gorm:"index" json:"revokedAt,omitempty"`
}

// AuditLog records mutating operations performed through the panel.
type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Time      time.Time `gorm:"index" json:"time"`
	UserID    uint      `gorm:"index" json:"userId"`
	Username  string    `gorm:"size:64" json:"username"`
	Method    string    `gorm:"size:8" json:"method"`
	Path      string    `gorm:"size:255" json:"path"`
	Status    int       `json:"status"`
	IP        string    `gorm:"size:64" json:"ip"`
	DurationMs int64    `json:"durationMs"`
}

// LoginLog records every login attempt (successful or not) so admins can
// audit who got in from where and spot brute-force attempts.
type LoginLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Time      time.Time `gorm:"index" json:"time"`
	Username  string    `gorm:"size:64;index" json:"username"`
	UserID    uint      `gorm:"index" json:"userId"`
	Success   bool      `json:"success"`
	Reason    string    `gorm:"size:128" json:"reason,omitempty"` // when Success=false
	IP        string    `gorm:"size:64" json:"ip"`
	UserAgent string    `gorm:"size:255" json:"userAgent"`
}

type Setting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"type:text" json:"value"`
}

// NodeGroup is a label admins attach to a set of daemons. New instances
// can target a group instead of a specific daemon; the panel resolves the
// best member at create time using the group scheduler.
type NodeGroup struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;size:64;not null" json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// NodeGroupMember is the many-to-many join between NodeGroup and Daemon.
// Multi-membership lets the same daemon belong to e.g. "mc-survival" and
// "high-mem" at once.
type NodeGroupMember struct {
	GroupID  uint `gorm:"primaryKey" json:"groupId"`
	DaemonID uint `gorm:"primaryKey" json:"daemonId"`
}

// DockerImageAlias lets admins assign a human-friendly display name
// to a docker image on a specific daemon. The alias is stored on the
// panel (not the daemon) so it survives daemon rebuilds and is
// independent of docker-layer labels.
type DockerImageAlias struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	DaemonID    uint   `gorm:"uniqueIndex:uniq_daemon_ref" json:"daemonId"`
	ImageRef    string `gorm:"uniqueIndex:uniq_daemon_ref" json:"imageRef"`
	DisplayName string `json:"displayName"`
}

func All() []any {
	return []any{
		&User{},
		&Daemon{},
		&InstancePermission{},
		&Task{},
		&APIKey{},
		&Setting{},
		&AuditLog{},
		&LoginLog{},
		&NodeGroup{},
		&NodeGroupMember{},
		&SSOProvider{},
		&SSOIdentity{},
		&DockerImageAlias{},
	}
}

// SSOProvider is one OIDC IdP an admin has registered. We support
// only standards-compliant OIDC (issuer + .well-known + JWKS); per-
// vendor OAuth2-only adapters (Feishu, GitHub, etc.) are out of
// scope. ClientSecret is encrypted at rest with the panel's
// secret-encryption.key (see internal/secrets).
type SSOProvider struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	// Name is the URL-safe slug used in callback paths
	// (/api/oauth/start/<name>). Stable identifier; changing it
	// breaks every IdP-side redirect_uri registration.
	Name            string    `gorm:"uniqueIndex;size:64;not null" json:"name"`
	DisplayName     string    `gorm:"size:128;not null" json:"displayName"`
	Enabled         bool      `gorm:"default:true" json:"enabled"`
	// Issuer is the OIDC issuer URL; we discover the rest via its
	// .well-known/openid-configuration document.
	Issuer          string    `gorm:"size:512;not null" json:"issuer"`
	ClientID        string    `gorm:"size:512;not null" json:"clientId"`
	// ClientSecretEnc is base64 of (nonce || AES-GCM(ciphertext)).
	// Never serialised to the frontend.
	ClientSecretEnc string    `gorm:"not null" json:"-"`
	// Scopes is space-separated OIDC scopes; "openid" is always added
	// regardless of what's listed here.
	Scopes          string    `gorm:"size:255;not null;default:'openid profile email'" json:"scopes"`
	// AutoCreate decides whether an unrecognised IdP user gets a fresh
	// local account on first login (true) or is rejected with
	// "account does not exist" (false).
	AutoCreate      bool      `gorm:"default:false" json:"autoCreate"`
	// DefaultRole is the role assigned to auto-created users. Has no
	// effect on existing users: their role stays whatever the panel
	// admin set it to.
	DefaultRole     Role      `gorm:"size:16;not null;default:user" json:"defaultRole"`
	// EmailDomains is a CSV whitelist (e.g. "yingxi.me,sub.yingxi.me").
	// Empty = no restriction. Email claims outside the list are
	// rejected at login time.
	EmailDomains    string    `gorm:"size:512" json:"emailDomains"`
	// TrustUnverifiedEmail relaxes the default policy that requires the
	// IdP to assert email_verified=true. Off by default (safe): an
	// attacker registering an unverified email at the IdP cannot match
	// a local account. Some self-hosted OIDC setups never emit the
	// claim — admins of those deployments can flip this on per-provider.
	TrustUnverifiedEmail bool `gorm:"default:false" json:"trustUnverifiedEmail"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// SSOIdentity records that a local user is bound to a specific IdP
// subject. One user can hold many identities (across providers); a
// given (provider, subject) pair maps to exactly one user.
type SSOIdentity struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `gorm:"index;not null" json:"userId"`
	ProviderID uint      `gorm:"index;not null;uniqueIndex:idx_sso_identity_provider_subject,priority:1" json:"providerId"`
	// Subject is the IdP-issued stable user id (`sub` claim). Treat
	// as opaque — never use the email claim for identity matching;
	// emails change, sub doesn't.
	Subject    string    `gorm:"size:255;not null;uniqueIndex:idx_sso_identity_provider_subject,priority:2" json:"subject"`
	// Email at the time of last login; informational only, not used
	// for matching after the initial bind.
	Email      string    `gorm:"size:255" json:"email"`
	LinkedAt   time.Time `json:"linkedAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
}
