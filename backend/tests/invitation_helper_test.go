package tests

import "net/http"

// acceptInvitations accepts every pending organisation invitation of u (operators
// join an organisation only by accepting an invitation).
func (e *env) acceptInvitations(u *user) {
	e.t.Helper()
	list := must(e.t, u.do(http.MethodGet, "/me/invitations", nil), 200, "my invitations")
	for _, x := range list.list("data") {
		id := x.(map[string]any)["id"].(string)
		must(e.t, u.do(http.MethodPost, "/invitations/"+id+"/accept", nil), 204, "accept invitation")
	}
}
