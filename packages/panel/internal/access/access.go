package access

import (
	"gorm.io/gorm"

	"github.com/taps/panel/internal/model"
)

// HasPerm reports whether the given user holds the requested permission bit
// for (daemonId, uuid). Admin always returns true.
func HasPerm(db *gorm.DB, userID uint, role model.Role, daemonID uint, uuid string, perm uint32) bool {
	if role == model.RoleAdmin {
		return true
	}
	var p model.InstancePermission
	err := db.Where("user_id = ? AND daemon_id = ? AND uuid = ?", userID, daemonID, uuid).First(&p).Error
	if err != nil {
		return false
	}
	if p.Perms == 0 {
		// legacy rows without explicit perms get the basic view+control set
		// so existing setups keep working after upgrade.
		return perm&(model.PermView|model.PermControl|model.PermFiles|model.PermTerminal) != 0
	}
	return p.Perms&perm == perm
}

// Allowed reports whether the given user can act on (daemonId, uuid) at all
// (any non-zero perm grant). Used for the "list view" filter.
func Allowed(db *gorm.DB, userID uint, role model.Role, daemonID uint, uuid string) bool {
	if role == model.RoleAdmin {
		return true
	}
	var count int64
	db.Model(&model.InstancePermission{}).
		Where("user_id = ? AND daemon_id = ? AND uuid = ?", userID, daemonID, uuid).
		Count(&count)
	return count > 0
}

// AllowedSet returns the set of (daemonId, uuid) the user can see, encoded as
// "daemonId|uuid" strings. Admin gets nil (meaning "all").
func AllowedSet(db *gorm.DB, userID uint, role model.Role) (map[string]struct{}, bool) {
	if role == model.RoleAdmin {
		return nil, true
	}
	var ps []model.InstancePermission
	db.Where("user_id = ?", userID).Find(&ps)
	out := make(map[string]struct{}, len(ps))
	for _, p := range ps {
		out[Key(p.DaemonID, p.UUID)] = struct{}{}
	}
	return out, false
}

func Key(daemonID uint, uuid string) string {
	return uintStr(daemonID) + "|" + uuid
}

func uintStr(u uint) string {
	if u == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for u > 0 {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
	}
	return string(b[i:])
}
