package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

func (d *Datastore) ApplyQueries(ctx context.Context, authorID uint, queries []*fleet.Query) (err error) {
	tx, err := d.writer.BeginTxx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin ApplyQueries transaction")
	}

	defer func() {
		if err != nil {
			rbErr := tx.Rollback()
			// It seems possible that there might be a case in
			// which the error we are dealing with here was thrown
			// by the call to tx.Commit(), and the docs suggest
			// this call would then result in sql.ErrTxDone.
			if rbErr != nil && rbErr != sql.ErrTxDone {
				panic(fmt.Sprintf("got err '%s' rolling back after err '%s'", rbErr, err))
			}
		}
	}()

	sql := `
		INSERT INTO queries (
			name,
			description,
			query,
			author_id,
			saved,
			observer_can_run
		) VALUES ( ?, ?, ?, ?, true, ? )
		ON DUPLICATE KEY UPDATE
			name = VALUES(name),
			description = VALUES(description),
			query = VALUES(query),
			author_id = VALUES(author_id),
			saved = VALUES(saved),
			observer_can_run = VALUES(observer_can_run)
	`
	stmt, err := tx.PrepareContext(ctx, sql)
	if err != nil {
		return errors.Wrap(err, "prepare ApplyQueries insert")
	}
	defer stmt.Close()

	for _, q := range queries {
		if q.Name == "" {
			return errors.New("query name must not be empty")
		}
		_, err := stmt.ExecContext(ctx, q.Name, q.Description, q.Query, authorID, q.ObserverCanRun)
		if err != nil {
			return errors.Wrap(err, "exec ApplyQueries insert")
		}
	}

	err = tx.Commit()
	return errors.Wrap(err, "commit ApplyQueries transaction")
}

func (d *Datastore) QueryByName(ctx context.Context, name string, opts ...fleet.OptionalArg) (*fleet.Query, error) {
	sqlStatement := `
		SELECT *
			FROM queries
			WHERE name = ?
	`
	var query fleet.Query
	err := sqlx.GetContext(ctx, d.reader, &query, sqlStatement, name)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, notFound("Query").WithName(name)
		}
		return nil, errors.Wrap(err, "selecting query by name")
	}

	if err := d.loadPacksForQueries(ctx, []*fleet.Query{&query}); err != nil {
		return nil, errors.Wrap(err, "loading packs for query")
	}

	return &query, nil
}

// NewQuery creates a New Query.
func (d *Datastore) NewQuery(ctx context.Context, query *fleet.Query, opts ...fleet.OptionalArg) (*fleet.Query, error) {
	sqlStatement := `
		INSERT INTO queries (
			name,
			description,
			query,
			saved,
			author_id,
			observer_can_run
		) VALUES ( ?, ?, ?, ?, ?, ? )
	`
	result, err := d.writer.ExecContext(ctx, sqlStatement, query.Name, query.Description, query.Query, query.Saved, query.AuthorID, query.ObserverCanRun)

	if err != nil && isDuplicate(err) {
		return nil, alreadyExists("Query", 0)
	} else if err != nil {
		return nil, errors.Wrap(err, "creating new Query")
	}

	id, _ := result.LastInsertId()
	query.ID = uint(id)
	query.Packs = []fleet.Pack{}
	return query, nil
}

// SaveQuery saves changes to a Query.
func (d *Datastore) SaveQuery(ctx context.Context, q *fleet.Query) error {
	sql := `
		UPDATE queries
			SET name = ?, description = ?, query = ?, author_id = ?, saved = ?, observer_can_run = ?
			WHERE id = ?
	`
	result, err := d.writer.ExecContext(ctx, sql, q.Name, q.Description, q.Query, q.AuthorID, q.Saved, q.ObserverCanRun, q.ID)
	if err != nil {
		return errors.Wrap(err, "updating query")
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "rows affected updating query")
	}
	if rows == 0 {
		return notFound("Query").WithID(q.ID)
	}

	return nil
}

// DeleteQuery deletes Query identified by Query.ID.
func (d *Datastore) DeleteQuery(ctx context.Context, name string) error {
	return d.deleteEntityByName(ctx, "queries", name)
}

// DeleteQueries deletes the existing query objects with the provided IDs. The
// number of deleted queries is returned along with any error.
func (d *Datastore) DeleteQueries(ctx context.Context, ids []uint) (uint, error) {
	return d.deleteEntities(ctx, "queries", ids)
}

// Query returns a single Query identified by id, if such exists.
func (d *Datastore) Query(ctx context.Context, id uint) (*fleet.Query, error) {
	sql := `
		SELECT q.*, COALESCE(NULLIF(u.name, ''), u.email, '') AS author_name
		FROM queries q
		LEFT JOIN users u
			ON q.author_id = u.id
		WHERE q.id = ?
	`
	query := &fleet.Query{}
	if err := sqlx.GetContext(ctx, d.reader, query, sql, id); err != nil {
		return nil, errors.Wrap(err, "selecting query")
	}

	if err := d.loadPacksForQueries(ctx, []*fleet.Query{query}); err != nil {
		return nil, errors.Wrap(err, "loading packs for queries")
	}

	return query, nil
}

// ListQueries returns a list of queries with sort order and results limit
// determined by passed in fleet.ListOptions
func (d *Datastore) ListQueries(ctx context.Context, opt fleet.ListOptions) ([]*fleet.Query, error) {
	sql := `
		SELECT q.*, COALESCE(u.name, '<deleted>') AS author_name
		FROM queries q
		LEFT JOIN users u
			ON q.author_id = u.id
		WHERE saved = true
	`
	sql = appendListOptionsToSQL(sql, opt)
	results := []*fleet.Query{}

	if err := sqlx.SelectContext(ctx, d.reader, &results, sql); err != nil {
		return nil, errors.Wrap(err, "listing queries")
	}

	if err := d.loadPacksForQueries(ctx, results); err != nil {
		return nil, errors.Wrap(err, "loading packs for queries")
	}

	return results, nil
}

// loadPacksForQueries loads the packs associated with the provided queries
func (d *Datastore) loadPacksForQueries(ctx context.Context, queries []*fleet.Query) error {
	if len(queries) == 0 {
		return nil
	}

	sql := `
		SELECT p.*, sq.query_name AS query_name
		FROM packs p
		JOIN scheduled_queries sq
			ON p.id = sq.pack_id
		WHERE query_name IN (?)
	`

	// Used to map the results
	name_queries := map[string]*fleet.Query{}
	// Used for the IN clause
	names := []string{}
	for _, q := range queries {
		q.Packs = make([]fleet.Pack, 0)
		names = append(names, q.Name)
		name_queries[q.Name] = q
	}

	query, args, err := sqlx.In(sql, names)
	if err != nil {
		return errors.Wrap(err, "building query in load packs for queries")
	}

	rows := []struct {
		QueryName string `db:"query_name"`
		fleet.Pack
	}{}

	err = sqlx.SelectContext(ctx, d.reader, &rows, query, args...)
	if err != nil {
		return errors.Wrap(err, "selecting load packs for queries")
	}

	for _, row := range rows {
		q := name_queries[row.QueryName]
		q.Packs = append(q.Packs, row.Pack)
	}

	return nil
}
