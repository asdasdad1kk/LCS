package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`

	// ParentID is the master project this one is a zone of ("" for top-level).
	ParentID string `json:"parentId"`
	// IntegratedCommit is the zone's main commit that was last copied into the
	// master; IntegratedMasterCommit is the master's main right after that copy.
	// Both are "" until the first integration.
	IntegratedCommit       string `json:"-"`
	IntegratedMasterCommit string `json:"-"`
	IntegratedAt           int64  `json:"integratedAt"`

	// Owner is the e-mail of whoever created the project.
	Owner string `json:"owner"`
}

type Drawing struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	Path        string `json:"path"`
	Name        string `json:"name"`
	MainBlob    string `json:"mainBlob"`
	Size        int64  `json:"size"`
	LayoutsJSON string `json:"-"`
	MetaJSON    string `json:"-"`
	UpdatedAt   int64  `json:"updatedAt"`
}

type Branch struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	Name       string `json:"name"`
	Ref        string `json:"ref"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	Status     string `json:"status"`
	BaseCommit string `json:"baseCommit"`
	HeadCommit string `json:"headCommit"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	MergedAt   int64  `json:"mergedAt"`

	AuthorEmail string `json:"authorEmail"`
	// TreeHash identifies the content of drawings/ at the branch tip; an
	// approval is only valid while it matches.
	TreeHash string `json:"-"`
}

// Approver is one node of a project's approval chain (position = order).
type Approver struct {
	Position int    `json:"position"`
	Email    string `json:"email"`
	Label    string `json:"label"`
}

// Approval is a decision recorded on a branch (mirror of .lcs/approvals/*.json).
type Approval struct {
	ID       string `json:"id"`
	BranchID string `json:"branchId"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Decision string `json:"decision"` // approved | rejected
	Comment  string `json:"comment"`
	Tree     string `json:"tree"`
	Commit   string `json:"commit"`
	At       int64  `json:"at"`
}

type BranchFile struct {
	BranchID  string `json:"branchId"`
	Path      string `json:"path"`
	BaseBlob  string `json:"baseBlob"`
	HeadBlob  string `json:"headBlob"`
	Changed   bool   `json:"changed"`
	IsBinary  bool   `json:"isBinary"`
	SizeBytes int64  `json:"sizeBytes"`
}

type Revision struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	DrawingPath string `json:"drawingPath"`
	CommitSHA   string `json:"commitSha"`
	BlobSHA     string `json:"blobSha"`
	Size        int64  `json:"size"`
	Author      string `json:"author"`
	Message     string `json:"message"`
	BranchID    string `json:"branchId"`
	CreatedAt   int64  `json:"createdAt"`
}

type Event struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"projectId"`
	BranchID  string `json:"branchId"`
	Kind      string `json:"kind"`
	Actor     string `json:"actor"`
	Payload   string `json:"payload"`
	CreatedAt int64  `json:"createdAt"`
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
	id           TEXT PRIMARY KEY,
	name         TEXT NOT NULL,
	description  TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS drawings (
	id           TEXT PRIMARY KEY,
	project_id   TEXT NOT NULL,
	path         TEXT NOT NULL,
	name         TEXT NOT NULL,
	main_blob    TEXT NOT NULL DEFAULT '',
	size         INTEGER NOT NULL DEFAULT 0,
	layouts_json TEXT NOT NULL DEFAULT '[]',
	meta_json    TEXT NOT NULL DEFAULT '{}',
	updated_at   INTEGER NOT NULL,
	UNIQUE(project_id, path)
);

CREATE TABLE IF NOT EXISTS branches (
	id          TEXT PRIMARY KEY,
	project_id  TEXT NOT NULL,
	name        TEXT NOT NULL UNIQUE,
	title       TEXT NOT NULL,
	author      TEXT NOT NULL,
	status      TEXT NOT NULL DEFAULT 'open',
	base_commit TEXT NOT NULL DEFAULT '',
	head_commit TEXT NOT NULL DEFAULT '',
	created_at  INTEGER NOT NULL,
	updated_at  INTEGER NOT NULL,
	merged_at   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS branch_files (
	branch_id TEXT NOT NULL,
	path TEXT NOT NULL,
	base_blob TEXT NOT NULL DEFAULT '',
	head_blob TEXT NOT NULL DEFAULT '',
	size_bytes INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (branch_id, path)
);

CREATE TABLE IF NOT EXISTS revisions (
	id           TEXT PRIMARY KEY,
	project_id   TEXT NOT NULL,
	drawing_path TEXT NOT NULL,
	commit_sha   TEXT NOT NULL,
	blob_sha     TEXT NOT NULL,
	size         INTEGER NOT NULL DEFAULT 0,
	author       TEXT NOT NULL DEFAULT '',
	message      TEXT NOT NULL DEFAULT '',
	branch_id    TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL DEFAULT '',
	branch_id  TEXT NOT NULL DEFAULT '',
	kind       TEXT NOT NULL,
	actor      TEXT NOT NULL DEFAULT '',
	payload    TEXT NOT NULL DEFAULT '{}',
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS project_approvers (
	project_id TEXT NOT NULL,
	position   INTEGER NOT NULL,
	email      TEXT NOT NULL,
	label      TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (project_id, position)
);

CREATE TABLE IF NOT EXISTS approvals (
	id         TEXT NOT NULL,
	branch_id  TEXT NOT NULL,
	email      TEXT NOT NULL,
	name       TEXT NOT NULL DEFAULT '',
	decision   TEXT NOT NULL,
	comment    TEXT NOT NULL DEFAULT '',
	tree       TEXT NOT NULL DEFAULT '',
	commit_sha TEXT NOT NULL DEFAULT '',
	at         INTEGER NOT NULL,
	PRIMARY KEY (branch_id, id)
);

CREATE INDEX IF NOT EXISTS idx_drawings_project ON drawings(project_id);
CREATE INDEX IF NOT EXISTS idx_branches_project ON branches(project_id, status);
CREATE INDEX IF NOT EXISTS idx_revisions_drawing ON revisions(project_id, drawing_path);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, id DESC);

CREATE TABLE IF NOT EXISTS analyses (
	id         TEXT NOT NULL,
	branch_id  TEXT NOT NULL,
	path       TEXT NOT NULL,
	commit_sha TEXT NOT NULL,
	author     TEXT NOT NULL DEFAULT '',
	at         INTEGER NOT NULL,
	summary    TEXT NOT NULL,
	PRIMARY KEY (branch_id, id)
);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migración de esquema: %w", err)
	}
	// Forward-compatible tweaks for databases created by earlier builds.
	for _, stmt := range []string{
		`ALTER TABLE branch_files ADD COLUMN size_bytes INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE projects ADD COLUMN parent_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE projects ADD COLUMN integrated_commit TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE projects ADD COLUMN integrated_master_commit TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE projects ADD COLUMN integrated_at INTEGER NOT NULL DEFAULT 0`,
		`CREATE INDEX IF NOT EXISTS idx_projects_parent ON projects(parent_id)`,
		`ALTER TABLE projects ADD COLUMN owner TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE branches ADD COLUMN author_email TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE branches ADD COLUMN tree_hash TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = db.Exec(stmt)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() int64 { return time.Now().UnixMilli() }

// ---------------------------------------------------------------- projects

const projectCols = `id,name,description,created_at,updated_at,parent_id,integrated_commit,integrated_master_commit,integrated_at,owner`

func scanProject(sc interface{ Scan(...any) error }) (*Project, error) {
	var p Project
	err := sc.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt,
		&p.ParentID, &p.IntegratedCommit, &p.IntegratedMasterCommit, &p.IntegratedAt, &p.Owner)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreateProject(p *Project) error {
	p.CreatedAt, p.UpdatedAt = now(), now()
	_, err := s.db.Exec(`INSERT INTO projects(id,name,description,created_at,updated_at,parent_id,owner) VALUES(?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Description, p.CreatedAt, p.UpdatedAt, p.ParentID, p.Owner)
	return err
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT ` + projectCols + ` FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ListChildren returns the zones of a master project.
func (s *Store) ListChildren(parentID string) ([]Project, error) {
	rows, err := s.db.Query(`SELECT `+projectCols+` FROM projects WHERE parent_id=? ORDER BY name`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) GetProject(id string) (*Project, error) {
	p, err := scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// SetIntegration records that the zone's main at zoneCommit was copied into the
// master, whose main is now masterCommit.
func (s *Store) SetIntegration(id, zoneCommit, masterCommit string) error {
	_, err := s.db.Exec(`UPDATE projects SET integrated_commit=?, integrated_master_commit=?, integrated_at=? WHERE id=?`,
		zoneCommit, masterCommit, now(), id)
	return err
}

func (s *Store) TouchProject(id string) error {
	_, err := s.db.Exec(`UPDATE projects SET updated_at=? WHERE id=?`, now(), id)
	return err
}

// ---------------------------------------------------------------- drawings

func (s *Store) UpsertDrawing(d *Drawing) error {
	d.UpdatedAt = now()
	_, err := s.db.Exec(`INSERT INTO drawings(id,project_id,path,name,main_blob,size,layouts_json,meta_json,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(project_id,path) DO UPDATE SET
			name=excluded.name, main_blob=excluded.main_blob, size=excluded.size, updated_at=excluded.updated_at`,
		d.ID, d.ProjectID, d.Path, d.Name, d.MainBlob, d.Size, d.LayoutsJSON, d.MetaJSON, d.UpdatedAt)
	return err
}

func (s *Store) ListDrawings(projectID string) ([]Drawing, error) {
	rows, err := s.db.Query(`SELECT id,project_id,path,name,main_blob,size,layouts_json,meta_json,updated_at
		FROM drawings WHERE project_id=? ORDER BY path`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Drawing{}
	for rows.Next() {
		var d Drawing
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Path, &d.Name, &d.MainBlob, &d.Size, &d.LayoutsJSON, &d.MetaJSON, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDrawing(id string) (*Drawing, error) {
	var d Drawing
	err := s.db.QueryRow(`SELECT id,project_id,path,name,main_blob,size,layouts_json,meta_json,updated_at
		FROM drawings WHERE id=?`, id).
		Scan(&d.ID, &d.ProjectID, &d.Path, &d.Name, &d.MainBlob, &d.Size, &d.LayoutsJSON, &d.MetaJSON, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &d, err
}

func (s *Store) GetDrawingByPath(projectID, path string) (*Drawing, error) {
	var d Drawing
	err := s.db.QueryRow(`SELECT id,project_id,path,name,main_blob,size,layouts_json,meta_json,updated_at
		FROM drawings WHERE project_id=? AND path=?`, projectID, path).
		Scan(&d.ID, &d.ProjectID, &d.Path, &d.Name, &d.MainBlob, &d.Size, &d.LayoutsJSON, &d.MetaJSON, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &d, err
}

func (s *Store) DeleteDrawing(id string) error {
	_, err := s.db.Exec(`DELETE FROM drawings WHERE id=?`, id)
	return err
}

func (s *Store) SetDrawingLayouts(id, layoutsJSON, metaJSON string) error {
	_, err := s.db.Exec(`UPDATE drawings SET layouts_json=?, meta_json=?, updated_at=? WHERE id=?`,
		layoutsJSON, metaJSON, now(), id)
	return err
}

// ---------------------------------------------------------------- branches

func (s *Store) CreateBranch(b *Branch) error {
	// Una tarea descubierta desde otro equipo conserva su fecha real de apertura.
	if b.CreatedAt == 0 {
		b.CreatedAt = now()
	}
	b.UpdatedAt = now()
	_, err := s.db.Exec(`INSERT INTO branches(id,project_id,name,title,author,status,base_commit,head_commit,created_at,updated_at,merged_at,author_email,tree_hash)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.ProjectID, b.Name, b.Title, b.Author, b.Status, b.BaseCommit, b.HeadCommit, b.CreatedAt, b.UpdatedAt, b.MergedAt, b.AuthorEmail, b.TreeHash)
	return err
}

func (s *Store) UpdateBranch(b *Branch) error {
	b.UpdatedAt = now()
	_, err := s.db.Exec(`UPDATE branches SET title=?,author=?,status=?,base_commit=?,head_commit=?,updated_at=?,merged_at=?,author_email=?,tree_hash=? WHERE id=?`,
		b.Title, b.Author, b.Status, b.BaseCommit, b.HeadCommit, b.UpdatedAt, b.MergedAt, b.AuthorEmail, b.TreeHash, b.ID)
	return err
}

const branchCols = `id,project_id,name,title,author,status,base_commit,head_commit,created_at,updated_at,merged_at,author_email,tree_hash`

func scanBranch(sc interface{ Scan(...any) error }) (*Branch, error) {
	var b Branch
	if err := sc.Scan(&b.ID, &b.ProjectID, &b.Name, &b.Title, &b.Author, &b.Status,
		&b.BaseCommit, &b.HeadCommit, &b.CreatedAt, &b.UpdatedAt, &b.MergedAt, &b.AuthorEmail, &b.TreeHash); err != nil {
		return nil, err
	}
	b.Ref = "refs/heads/" + b.Name
	return &b, nil
}

func (s *Store) GetBranch(id string) (*Branch, error) {
	b, err := scanBranch(s.db.QueryRow(`SELECT `+branchCols+` FROM branches WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

func (s *Store) GetBranchByName(name string) (*Branch, error) {
	b, err := scanBranch(s.db.QueryRow(`SELECT `+branchCols+` FROM branches WHERE name=?`, name))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

func (s *Store) ListBranches(projectID, status string) ([]Branch, error) {
	q := `SELECT ` + branchCols + ` FROM branches WHERE project_id=?`
	args := []any{projectID}
	if status != "" && status != "all" {
		if status == "active" {
			q += ` AND status IN ('open','review')`
		} else {
			q += ` AND status=?`
			args = append(args, status)
		}
	}
	q += ` ORDER BY updated_at DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Branch{}
	for rows.Next() {
		b, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *Store) SetBranchFiles(branchID string, files []BranchFile) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM branch_files WHERE branch_id=?`, branchID); err != nil {
		return err
	}
	for _, f := range files {
		if _, err := tx.Exec(`INSERT INTO branch_files(branch_id,path,base_blob,head_blob,size_bytes) VALUES(?,?,?,?,?)`,
			branchID, f.Path, f.BaseBlob, f.HeadBlob, f.SizeBytes); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListBranchFiles(branchID string) ([]BranchFile, error) {
	rows, err := s.db.Query(`SELECT branch_id,path,base_blob,head_blob,size_bytes FROM branch_files WHERE branch_id=? ORDER BY path`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BranchFile{}
	for rows.Next() {
		var f BranchFile
		if err := rows.Scan(&f.BranchID, &f.Path, &f.BaseBlob, &f.HeadBlob, &f.SizeBytes); err != nil {
			return nil, err
		}
		f.Changed = f.BaseBlob != f.HeadBlob
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- revisions

func (s *Store) AddRevision(r *Revision) error {
	if r.CreatedAt == 0 {
		r.CreatedAt = now()
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO revisions(id,project_id,drawing_path,commit_sha,blob_sha,size,author,message,branch_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ProjectID, r.DrawingPath, r.CommitSHA, r.BlobSHA, r.Size, r.Author, r.Message, r.BranchID, r.CreatedAt)
	return err
}

func (s *Store) ListRevisions(projectID, drawingPath string, limit int) ([]Revision, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id,project_id,drawing_path,commit_sha,blob_sha,size,author,message,branch_id,created_at
		FROM revisions WHERE project_id=?`
	args := []any{projectID}
	if drawingPath != "" {
		q += ` AND drawing_path=?`
		args = append(args, drawingPath)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.DrawingPath, &r.CommitSHA, &r.BlobSHA, &r.Size,
			&r.Author, &r.Message, &r.BranchID, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- events

func (s *Store) AddEvent(projectID, branchID, kind, actor string, payload any) error {
	raw, _ := json.Marshal(payload)
	_, err := s.db.Exec(`INSERT INTO events(project_id,branch_id,kind,actor,payload,created_at) VALUES(?,?,?,?,?,?)`,
		projectID, branchID, kind, actor, string(raw), now())
	return err
}

func (s *Store) ListEvents(projectID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 40
	}
	q := `SELECT id,project_id,branch_id,kind,actor,payload,created_at FROM events`
	args := []any{}
	if projectID != "" {
		q += ` WHERE project_id=?`
		args = append(args, projectID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.BranchID, &e.Kind, &e.Actor, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListAllBranches returns branches across every project, with the project name.
func (s *Store) ListAllBranches(status string) ([]BranchRow, error) {
	q := `SELECT b.id,b.project_id,b.name,b.title,b.author,b.status,b.base_commit,b.head_commit,
	             b.created_at,b.updated_at,b.merged_at,b.author_email,b.tree_hash,p.name
	      FROM branches b JOIN projects p ON p.id = b.project_id`
	args := []any{}
	if status != "" && status != "all" {
		if status == "active" {
			q += ` WHERE b.status IN ('open','review')`
		} else {
			q += ` WHERE b.status = ?`
			args = append(args, status)
		}
	}
	q += ` ORDER BY b.updated_at DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BranchRow{}
	for rows.Next() {
		var b BranchRow
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Name, &b.Title, &b.Author, &b.Status,
			&b.BaseCommit, &b.HeadCommit, &b.CreatedAt, &b.UpdatedAt, &b.MergedAt, &b.AuthorEmail, &b.TreeHash, &b.ProjectName); err != nil {
			return nil, err
		}
		b.Ref = "refs/heads/" + b.Name
		out = append(out, b)
	}
	return out, rows.Err()
}

// BranchRow is a branch enriched with its project name.
type BranchRow struct {
	Branch
	ProjectName string `json:"projectName"`
}

// ---------------------------------------------------------------- approvals

// SetProjectOwner records who created the project.
func (s *Store) SetProjectOwner(id, owner string) error {
	_, err := s.db.Exec(`UPDATE projects SET owner=? WHERE id=?`, owner, id)
	return err
}

// ReplaceApprovers stores the project's approval chain, in order.
func (s *Store) ReplaceApprovers(projectID string, chain []Approver) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM project_approvers WHERE project_id=?`, projectID); err != nil {
		return err
	}
	for i, a := range chain {
		if _, err := tx.Exec(`INSERT INTO project_approvers(project_id,position,email,label) VALUES(?,?,?,?)`,
			projectID, i, a.Email, a.Label); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListApprovers(projectID string) ([]Approver, error) {
	rows, err := s.db.Query(`SELECT position,email,label FROM project_approvers WHERE project_id=? ORDER BY position`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Approver{}
	for rows.Next() {
		var a Approver
		if err := rows.Scan(&a.Position, &a.Email, &a.Label); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReplaceApprovals stores every decision recorded on a branch.
func (s *Store) ReplaceApprovals(branchID string, list []Approval) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM approvals WHERE branch_id=?`, branchID); err != nil {
		return err
	}
	for _, a := range list {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO approvals(id,branch_id,email,name,decision,comment,tree,commit_sha,at)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			a.ID, branchID, a.Email, a.Name, a.Decision, a.Comment, a.Tree, a.Commit, a.At); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListApprovals returns a branch's decisions, oldest first.
func (s *Store) ListApprovals(branchID string) ([]Approval, error) {
	rows, err := s.db.Query(`SELECT id,branch_id,email,name,decision,comment,tree,commit_sha,at
		FROM approvals WHERE branch_id=? ORDER BY at, id`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Approval{}
	for rows.Next() {
		var a Approval
		if err := rows.Scan(&a.ID, &a.BranchID, &a.Email, &a.Name, &a.Decision, &a.Comment, &a.Tree, &a.Commit, &a.At); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteProject borra el proyecto y todo lo que cuelga de él en el índice local
// (planos, tareas, aprobaciones, revisiones, eventos). No toca git.
func (s *Store) DeleteProject(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []string{
		`DELETE FROM approvals WHERE branch_id IN (SELECT id FROM branches WHERE project_id=?)`,
		`DELETE FROM branch_files WHERE branch_id IN (SELECT id FROM branches WHERE project_id=?)`,
		`DELETE FROM analyses WHERE branch_id IN (SELECT id FROM branches WHERE project_id=?)`,
		`DELETE FROM branches WHERE project_id=?`,
		`DELETE FROM revisions WHERE project_id=?`,
		`DELETE FROM drawings WHERE project_id=?`,
		`DELETE FROM project_approvers WHERE project_id=?`,
		`DELETE FROM events WHERE project_id=?`,
		`DELETE FROM projects WHERE id=?`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListEventsFor devuelve los eventos de varios proyectos (todos si ids está vacío).
func (s *Store) ListEventsFor(ids []string, limit int) ([]Event, error) {
	if len(ids) == 0 {
		return s.ListEvents("", limit)
	}
	if limit <= 0 {
		limit = 40
	}
	ph := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	for i, id := range ids {
		ph[i] = "?"
		args = append(args, id)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT id,project_id,branch_id,kind,actor,payload,created_at FROM events
		WHERE project_id IN (`+strings.Join(ph, ",")+`) ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.BranchID, &e.Kind, &e.Actor, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Analysis es el resumen de lo que cambió en un plano de una tarea (el análisis
// completo vive en git, en .lcs/analysis/<id>.json).
type Analysis struct {
	ID       string `json:"id"`
	BranchID string `json:"branchId"`
	Path     string `json:"path"`
	Commit   string `json:"commit"`
	Author   string `json:"author"`
	At       int64  `json:"at"`
	// Summary es el JSON de resumen tal como lo calculó el visor.
	Summary json.RawMessage `json:"summary"`
}

func (s *Store) ReplaceAnalyses(branchID string, list []Analysis) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM analyses WHERE branch_id=?`, branchID); err != nil {
		return err
	}
	for _, a := range list {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO analyses(id,branch_id,path,commit_sha,author,at,summary) VALUES(?,?,?,?,?,?,?)`,
			a.ID, branchID, a.Path, a.Commit, a.Author, a.At, string(a.Summary)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AddAnalysis(a Analysis) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO analyses(id,branch_id,path,commit_sha,author,at,summary) VALUES(?,?,?,?,?,?,?)`,
		a.ID, a.BranchID, a.Path, a.Commit, a.Author, a.At, string(a.Summary))
	return err
}

func (s *Store) ListAnalyses(branchID string) ([]Analysis, error) {
	rows, err := s.db.Query(`SELECT id,branch_id,path,commit_sha,author,at,summary FROM analyses WHERE branch_id=? ORDER BY at ASC`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Analysis{}
	for rows.Next() {
		var a Analysis
		var sum string
		if err := rows.Scan(&a.ID, &a.BranchID, &a.Path, &a.Commit, &a.Author, &a.At, &sum); err != nil {
			return nil, err
		}
		a.Summary = json.RawMessage(sum)
		out = append(out, a)
	}
	return out, rows.Err()
}
