package store

// ClampLimit 把分页 limit 归一到 [1, max];越界(<=0 或 >max)返回 def。
// 统一各列表查询(audit / request_logs / usage 等)的分页归一逻辑,避免各处手写不同上下界与默认值。
func ClampLimit(limit, def, max int) int {
	if limit <= 0 || limit > max {
		return def
	}
	return limit
}
