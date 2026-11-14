package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

CREATE INDEX IF NOT EXISTS idx_drawings_project ON drawings(project_id);
CREATE INDEX IF NOT EXISTS idx_branches_project ON branches(project_id, status);
CREATE INDEX IF NOT EXISTS idx_revisions_drawing ON revisions(project_id, drawing_path);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, id DESC);
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
	} {
		_, _ = db.Exec(stmt)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() int64 { return time.Now().UnixMilli() }

// ---------------------------------------------------------------- projects

func (s *Store) CreateProject(p *Project) error {
	p.CreatedAt, p.UpdatedAt = now(), now()
	_, err := s.db.Exec(`INSERT INTO projects(id,name,description,created_at,updated_at) VALUES(?,?,?,?,?)`,
		p.ID, p.Name, p.Description, p.CreatedAt, p.UpdatedAt)
	return err
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT id,name,description,created_at,updated_at FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProject(id string) (*Project, error) {
	var p Project
	err := s.db.QueryRow(`SELECT id,name,description,created_at,updated_at FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &p, err
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

func (s *Store) SetDrawingLayouts(id, layoutsJSON, metaJSON string) error {
	_, err := s.db.Exec(`UPDATE drawings SET layouts_json=?, meta_json=?, updated_at=? WHERE id=?`,
		layoutsJSON, metaJSON, now(), id)
	return err
}

// ---------------------------------------------------------------- branches

func (s *Store) CreateBranch(b *Branch) error {
	b.CreatedAt, b.UpdatedAt = now(), now()
	_, err := s.db.Exec(`INSERT INTO branches(id,project_id,name,title,author,status,base_commit,head_commit,created_at,updated_at,merged_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.ProjectID, b.Name, b.Title, b.Author, b.Status, b.BaseCommit, b.HeadCommit, b.CreatedAt, b.UpdatedAt, b.MergedAt)
	return err
}

func (s *Store) UpdateBranch(b *Branch) error {
	b.UpdatedAt = now()
	_, err := s.db.Exec(`UPDATE branches SET title=?,author=?,status=?,base_commit=?,head_commit=?,updated_at=?,merged_at=? WHERE id=?`,
		b.Title, b.Author, b.Status, b.BaseCommit, b.HeadCommit, b.UpdatedAt, b.MergedAt, b.ID)
	return err
}

const branchCols = `id,project_id,name,title,author,status,base_commit,head_commit,created_at,updated_at,merged_at`

func scanBranch(sc interface{ Scan(...any) error }) (*Branch, error) {
	var b Branch
	if err := sc.Scan(&b.ID, &b.ProjectID, &b.Name, &b.Title, &b.Author, &b.Status,
		&b.BaseCommit, &b.HeadCommit, &b.CreatedAt, &b.UpdatedAt, &b.MergedAt); err != nil {
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
	             b.created_at,b.updated_at,b.merged_at,p.name
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
			&b.BaseCommit, &b.HeadCommit, &b.CreatedAt, &b.UpdatedAt, &b.MergedAt, &b.ProjectName); err != nil {
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
