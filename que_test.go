package que

import (
	"context"
	"testing"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testConnConfig = func() pgxpool.Config {
	poolConfig, _ := pgxpool.ParseConfig("host=localhost database=que-go-test user=postgres password=postgres")
	return *poolConfig
}

func openTestClientMaxConns(t testing.TB, maxConnections int32) *Client {
	connPoolConfig := testConnConfig()
	connPoolConfig.MaxConns = maxConnections
	connPoolConfig.AfterConnect = PrepareStatements

	pool, err := pgxpool.NewWithConfig(context.Background(), &connPoolConfig)
	if err != nil {
		t.Fatal(err)
	}
	return NewClient(pool)
}

func openTestClient(t testing.TB) *Client {
	return openTestClientMaxConns(t, 5)
}

func truncateAndClose(ctx context.Context, pool *pgxpool.Pool) {
	if _, err := pool.Exec(ctx, "TRUNCATE TABLE que_jobs"); err != nil {
		panic(err)
	}
	pool.Close()
}

func findOneJob(ctx context.Context, q queryable) (*Job, error) {
	findSQL := `
	SELECT priority, run_at, job_id, job_class, args, error_count, last_error, queue
	FROM que_jobs LIMIT 1`

	j := &Job{}
	err := q.QueryRow(ctx, findSQL).Scan(
		&j.Priority,
		&j.RunAt,
		&j.ID,
		&j.Type,
		&j.Args,
		&j.ErrorCount,
		&j.LastError,
		&j.Queue,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}
