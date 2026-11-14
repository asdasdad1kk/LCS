package app

import (
	"testing"

	"lcs/internal/store"
)

func chainOf(emails ...string) []store.Approver {
	out := make([]store.Approver, len(emails))
	for i, e := range emails {
		out[i] = store.Approver{Position: i, Email: e}
	}
	return out
}

func dec(email, decision, tree string, at int64) store.Approval {
	return store.Approval{ID: email + decision, Email: email, Decision: decision, Tree: tree, At: at}
}

func statuses(st ReviewState) []NodeStatus {
	out := []NodeStatus{}
	for _, n := range st.Nodes {
		out = append(out, n.Status)
	}
	return out
}

func same(a, b []NodeStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReviewNoChainMeansNoGate(t *testing.T) {
	st := computeReview(nil, nil, "t1", "a@x.com", true)
	if st.Required || !st.Complete || st.Next != -1 {
		t.Fatalf("sin aprobadores no debe haber compuerta: %+v", st)
	}
}

func TestReviewIsSequential(t *testing.T) {
	chain := chainOf("a@x.com", "b@x.com", "c@x.com")

	st := computeReview(chain, nil, "t1", "z@x.com", true)
	if !same(statuses(st), []NodeStatus{NodePending, NodeWaiting, NodeWaiting}) || st.Next != 0 || st.Complete {
		t.Fatalf("inicio: %v next=%d", statuses(st), st.Next)
	}
	if !st.Nodes[0].Active || st.Nodes[1].Active {
		t.Fatal("solo el primer nodo debe estar activo")
	}

	st = computeReview(chain, []store.Approval{dec("a@x.com", "approved", "t1", 1)}, "t1", "z@x.com", true)
	if !same(statuses(st), []NodeStatus{NodeApproved, NodePending, NodeWaiting}) || st.Next != 1 {
		t.Fatalf("tras a: %v next=%d", statuses(st), st.Next)
	}

	// Una aprobación fuera de orden (c antes que b) no cuenta como avance.
	st = computeReview(chain, []store.Approval{
		dec("a@x.com", "approved", "t1", 1), dec("c@x.com", "approved", "t1", 2),
	}, "t1", "z@x.com", true)
	if st.Next != 1 || st.Complete {
		t.Fatalf("c no puede saltarse a b: %v next=%d", statuses(st), st.Next)
	}

	st = computeReview(chain, []store.Approval{
		dec("a@x.com", "approved", "t1", 1), dec("b@x.com", "approved", "t1", 2), dec("c@x.com", "approved", "t1", 3),
	}, "t1", "z@x.com", true)
	if !st.Complete || st.Next != -1 {
		t.Fatalf("todos aprobaron: %v", statuses(st))
	}
}

func TestReviewNewVersionInvalidatesEverything(t *testing.T) {
	chain := chainOf("a@x.com", "b@x.com")
	old := []store.Approval{dec("a@x.com", "approved", "t1", 1), dec("b@x.com", "approved", "t1", 2)}

	if st := computeReview(chain, old, "t1", "", true); !st.Complete {
		t.Fatal("con la versión revisada debería estar completo")
	}
	st := computeReview(chain, old, "t2", "", true) // se subió otro plano
	if st.Complete || st.Next != 0 || !st.Nodes[0].Stale || !st.Nodes[1].Stale {
		t.Fatalf("las aprobaciones viejas no deben valer: %v next=%d", statuses(st), st.Next)
	}
}

func TestReviewRejectionBlocksUntilNewVersion(t *testing.T) {
	chain := chainOf("a@x.com", "b@x.com")
	ds := []store.Approval{dec("a@x.com", "approved", "t1", 1), dec("b@x.com", "rejected", "t1", 2)}

	st := computeReview(chain, ds, "t1", "", true)
	if !same(statuses(st), []NodeStatus{NodeApproved, NodeRejected}) || st.Complete || st.Next != 1 || !st.Nodes[1].Active {
		t.Fatalf("rechazo: %v next=%d", statuses(st), st.Next)
	}
	// Con una versión nueva el rechazo queda obsoleto y la cadena arranca de cero.
	st = computeReview(chain, ds, "t2", "", true)
	if !same(statuses(st), []NodeStatus{NodePending, NodeWaiting}) {
		t.Fatalf("tras nueva versión: %v", statuses(st))
	}
}

func TestReviewLatestDecisionWins(t *testing.T) {
	chain := chainOf("a@x.com")
	ds := []store.Approval{dec("a@x.com", "rejected", "t1", 1), dec("a@x.com", "approved", "t1", 2)}
	if st := computeReview(chain, ds, "t1", "", true); !st.Complete {
		t.Fatal("quien cambia de opinión debe valer con su última decisión")
	}
}

func TestReviewEmailsAreCaseInsensitive(t *testing.T) {
	chain := chainOf("Ana@X.com")
	ds := []store.Approval{dec("ana@x.COM", "approved", "t1", 1)}
	st := computeReview(chain, ds, "t1", "ANA@x.com", true)
	if !st.Complete || !st.Nodes[0].IsAuthor {
		t.Fatalf("%+v", st.Nodes[0])
	}
}

func TestReviewWithoutDrawingsTreeNothingIsValid(t *testing.T) {
	chain := chainOf("a@x.com")
	st := computeReview(chain, []store.Approval{dec("a@x.com", "approved", "", 1)}, "", "", false)
	if st.Complete {
		t.Fatal("sin contenido que revisar no puede estar aprobado")
	}
}
