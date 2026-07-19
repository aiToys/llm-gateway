package auth

import "testing"

// TestRoleHelpers 锁定平台/租户管理员的判定契约,防止角色字符串漂移。
func TestRoleHelpers(t *testing.T) {
	cases := []struct {
		role             string
		platform, tenant bool
	}{
		{RolePlatformAdmin, true, false},
		{RoleTenantAdmin, false, true},
		{RoleMember, false, false},
	}
	for _, c := range cases {
		s := Subject{Role: c.role}
		if s.IsPlatformAdmin() != c.platform {
			t.Errorf("%s IsPlatformAdmin=%v want %v", c.role, s.IsPlatformAdmin(), c.platform)
		}
		if s.IsTenantAdmin() != c.tenant {
			t.Errorf("%s IsTenantAdmin=%v want %v", c.role, s.IsTenantAdmin(), c.tenant)
		}
		if got := s.IsAdmin(); got != (c.platform || c.tenant) {
			t.Errorf("%s IsAdmin=%v want %v", c.role, got, c.platform || c.tenant)
		}
	}
}
