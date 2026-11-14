package lookup

import (
	"context"
	"database/sql"
)

func Find(ctx context.Context, db *sql.DB, owner, name string) (*sql.Rows, error) {
	const statement = "SELECT id FROM resources WHERE owner_id = $1 AND name = $2"
	return db.QueryContext(ctx, statement, owner, name)
}
