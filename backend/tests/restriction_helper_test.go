package tests

// restrict applies an account restriction directly (admin accounts are read-only through the API).
func (e *env) restrict(userID, kind string) string {
	e.t.Helper()
	return e.query1(`INSERT INTO account_restrictions (user_id, kind, reason) VALUES ($1, $2, 'Under investigation') RETURNING id::text`, userID, kind)
}

func (e *env) liftRestriction(id, by string) {
	e.t.Helper()
	e.exec(`UPDATE account_restrictions SET lifted_at = now(), lifted_by = $2, lift_reason = 'Cleared' WHERE id = $1`, id, by)
}
