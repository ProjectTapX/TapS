package api

import (
	"strings"
	"time"

	"github.com/taps/panel/internal/model"
)

// DTOs for response bodies. Even though model.User.PasswordHash and
// model.Daemon.Token both carry `json:"-"` (verified — they are not
// serialised by the default marshaller), we route every response
// through these explicit DTOs so a future dev who adds a new
// sensitive field on the model can't accidentally leak it via
// `c.JSON(..., u)` somewhere. The DTOs encode an allowlist of fields
// to ship; new model fields are silently dropped here unless this
// file is touched too.

type userDTO struct {
	ID                 uint      `json:"id"`
	Username           string    `json:"username"`
	Email              string    `json:"email"`
	Role               model.Role `json:"role"`
	MustChangePassword bool      `json:"mustChangePassword"`
	HasPassword        bool      `json:"hasPassword"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

func userToDTO(u *model.User) userDTO {
	if u == nil {
		return userDTO{}
	}
	return userDTO{
		ID:                 u.ID,
		Username:           u.Username,
		Email:              u.Email,
		Role:               u.Role,
		MustChangePassword: u.MustChangePassword,
		HasPassword:        u.HasPassword,
		CreatedAt:          u.CreatedAt,
		UpdatedAt:          u.UpdatedAt,
	}
}

func usersToDTOs(rows []model.User) []userDTO {
	out := make([]userDTO, 0, len(rows))
	for i := range rows {
		out = append(out, userToDTO(&rows[i]))
	}
	return out
}

// daemonDTO is the admin-facing view: includes Address (the real
// management host:port) for the daemon list / edit form. Token is
// provisioned at create time only (the admin generates it, sees it
// once, then it's write-only via PUT). Non-admin callers MUST go
// through daemonPublicDTO instead — Address is sensitive infra info
// and should never reach a non-admin user.
type daemonDTO struct {
	ID              uint      `json:"id"`
	Name            string    `json:"name"`
	Address         string    `json:"address"`
	DisplayHost     string    `json:"displayHost"`
	PortMin         int       `json:"portMin"`
	PortMax         int       `json:"portMax"`
	LastSeen        time.Time `json:"lastSeen"`
	CreatedAt       time.Time `json:"createdAt"`
	CertFingerprint string    `json:"certFingerprint,omitempty"`
}

func daemonToDTO(d *model.Daemon) daemonDTO {
	if d == nil {
		return daemonDTO{}
	}
	return daemonDTO{
		ID:              d.ID,
		Name:            d.Name,
		Address:         d.Address,
		DisplayHost:     d.DisplayHost,
		PortMin:         d.PortMin,
		PortMax:         d.PortMax,
		LastSeen:        d.LastSeen,
		CreatedAt:       d.CreatedAt,
		CertFingerprint: d.CertFingerprint,
	}
}

// daemonPublicDTO is the non-admin-safe view: omits Address so the
// real daemon management endpoint is never exposed to regular users.
// Used by GET /api/daemons/:id/public.
type daemonPublicDTO struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	DisplayHost string `json:"displayHost"`
}

func daemonToPublicDTO(d *model.Daemon) daemonPublicDTO {
	if d == nil {
		return daemonPublicDTO{}
	}
	host := d.DisplayHost
	if host == "" {
		// Fall back to the host portion of the management Address so the
		// instance detail page can still render *something* the player can
		// connect to. We deliberately strip the port so the management
		// endpoint isn't exposed.
		if i := strings.LastIndex(d.Address, ":"); i > 0 {
			host = d.Address[:i]
		} else {
			host = d.Address
		}
	}
	return daemonPublicDTO{
		ID:          d.ID,
		Name:        d.Name,
		DisplayHost: host,
	}
}
