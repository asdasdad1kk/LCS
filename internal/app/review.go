package app

import (
	"fmt"
	"strings"

	"lcs/internal/identity"
	"lcs/internal/store"
)

// Aprobación en cadena, como un pull request con varios nodos.
//
// Cada proyecto define una lista ordenada de aprobadores (correos). Una tarea
// solo se puede integrar cuando TODOS aprobaron, en orden: el nodo N no puede
// decidir hasta que el N-1 aprobó. Una decisión vale para el contenido exacto
// de drawings/ que se revisó (Tree): si se sube otra versión de un plano, las
// decisiones anteriores quedan obsoletas y el flujo vuelve a empezar.

type NodeStatus string

const (
	NodeApproved NodeStatus = "approved" // aprobó esta versión
	NodeRejected NodeStatus = "rejected" // rechazó esta versión
	NodePending  NodeStatus = "pending"  // le toca decidir
	NodeWaiting  NodeStatus = "waiting"  // espera a un nodo anterior
)

type Node struct {
	Position int        `json:"position"`
	Email    string     `json:"email"`
	Label    string     `json:"label"`
	Status   NodeStatus `json:"status"`
	// Active: este es el nodo que puede decidir ahora.
	Active bool `json:"active"`
	// Stale: tiene una decisión, pero de una versión anterior de los planos.
	Stale    bool            `json:"stale"`
	IsAuthor bool            `json:"isAuthor"`
	IsMe     bool            `json:"isMe"`
	Decision *store.Approval `json:"decision,omitempty"`
}

type ReviewState struct {
	// Required: el proyecto tiene aprobadores. Sin ellos no hay compuerta.
	Required   bool `json:"required"`
	HasChanges bool `json:"hasChanges"`
	// Uploaded: ya se subió al menos una versión (aunque sea idéntica a main).
	Uploaded bool `json:"uploaded"`
	// Complete: se puede integrar (no hay cadena, o todos aprobaron).
	Complete bool   `json:"complete"`
	Nodes    []Node `json:"nodes"`
	// Next: índice del nodo que debe decidir; -1 si ya no queda ninguno.
	Next int `json:"next"`
}

// computeReview calcula el estado de la cadena. decisions debe venir de la más
// vieja a la más nueva; para cada correo vale la última.
func computeReview(chain []store.Approver, decisions []store.Approval, tree, authorEmail string, hasChanges bool) ReviewState {
	latest := map[string]store.Approval{}
	for _, d := range decisions {
		latest[identity.Normalize(d.Email)] = d
	}
	author := identity.Normalize(authorEmail)

	st := ReviewState{Required: len(chain) > 0, HasChanges: hasChanges, Next: -1, Nodes: make([]Node, 0, len(chain))}
	blocked := false
	for i, a := range chain {
		email := identity.Normalize(a.Email)
		n := Node{Position: i, Email: email, Label: a.Label, IsAuthor: author != "" && email == author}

		valid := false
		if d, ok := latest[email]; ok {
			dd := d
			n.Decision = &dd
			valid = tree != "" && d.Tree == tree
			n.Stale = !valid
		}
		switch {
		case valid && n.Decision.Decision == "approved":
			n.Status = NodeApproved
		case valid && n.Decision.Decision == "rejected":
			n.Status = NodeRejected
		case blocked:
			n.Status = NodeWaiting
		default:
			n.Status = NodePending
		}
		if n.Status != NodeApproved {
			if !blocked {
				st.Next = i
				n.Active = true
			}
			blocked = true
		}
		st.Nodes = append(st.Nodes, n)
	}
	st.Complete = !st.Required || st.Next == -1
	return st
}

// Missing describe, para un mensaje de error, qué falta para integrar.
func (st ReviewState) Missing() string {
	parts := []string{}
	for _, n := range st.Nodes {
		switch n.Status {
		case NodeApproved:
		case NodeRejected:
			parts = append(parts, n.Email+" (rechazó)")
		case NodePending:
			parts = append(parts, n.Email+" (le toca)")
		default:
			parts = append(parts, n.Email)
		}
	}
	return strings.Join(parts, ", ")
}

// ErrNeedsApproval se devuelve al integrar una tarea que todavía no tiene todas
// las aprobaciones.
type ErrNeedsApproval struct{ State ReviewState }

func (e *ErrNeedsApproval) Error() string {
	if !e.State.HasChanges {
		return "la tarea no tiene cambios para aprobar"
	}
	return fmt.Sprintf("faltan aprobaciones: %s", e.State.Missing())
}

// markMe y los helpers de arriba no necesitan git: toda la lógica de orden y
// obsolescencia vive en computeReview para poder probarla sola.
func (st *ReviewState) markMe(email string) {
	me := identity.Normalize(email)
	for i := range st.Nodes {
		st.Nodes[i].IsMe = st.Nodes[i].Email == me
	}
}

// myIndex devuelve la posición del usuario en la cadena, o -1.
func (st ReviewState) myIndex(email string) int {
	me := identity.Normalize(email)
	for i, n := range st.Nodes {
		if n.Email == me {
			return i
		}
	}
	return -1
}
