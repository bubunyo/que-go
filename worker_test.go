package que

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"testing"
)

func init() {
	log.SetOutput(io.Discard)
}

func TestWorkerWorkOne(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(context.Background(), c.pool)

	success := false
	wm := WorkMap{
		"MyJob": func(j *Job) error {
			success = true
			return nil
		},
	}
	w := NewWorker(c, wm)

	didWork := w.WorkOne()
	if didWork {
		t.Errorf("want didWork=false when no job was queued")
	}

	if err := c.Enqueue(context.Background(), &Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	didWork = w.WorkOne()
	if !didWork {
		t.Errorf("want didWork=true")
	}
	if !success {
		t.Errorf("want success=true")
	}
}

func TestWorkerShutdown(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(context.Background(), c.pool)

	w := NewWorker(c, WorkMap{})
	finished := false
	go func() {
		w.Work()
		finished = true
	}()
	w.Shutdown()
	if !finished {
		t.Errorf("want finished=true")
	}
	if !w.done {
		t.Errorf("want w.done=true")
	}
}

func BenchmarkWorker(b *testing.B) {
	c := openTestClient(b)
	log.SetOutput(io.Discard)
	defer func() {
		log.SetOutput(os.Stdout)
	}()
	defer truncateAndClose(context.Background(), c.pool)

	w := NewWorker(c, WorkMap{"Nil": nilWorker})

	for i := 0; i < b.N; i++ {
		if err := c.Enqueue(context.Background(), &Job{Type: "Nil"}); err != nil {
			log.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.WorkOne()
	}
}

func nilWorker(j *Job) error {
	return nil
}

func TestWorkerWorkReturnsError(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(context.Background(), c.pool)

	called := 0
	wm := WorkMap{
		"MyJob": func(j *Job) error {
			called++
			return fmt.Errorf("the error msg")
		},
	}
	w := NewWorker(c, wm)

	didWork := w.WorkOne()
	if didWork {
		t.Errorf("want didWork=false when no job was queued")
	}

	if err := c.Enqueue(context.Background(), &Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	didWork = w.WorkOne()
	if !didWork {
		t.Errorf("want didWork=true")
	}
	if called != 1 {
		t.Errorf("want called=1 was: %d", called)
	}

	tx, err := c.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	j, err := findOneJob(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	if j.ErrorCount != 1 {
		t.Errorf("want ErrorCount=1 was %d", j.ErrorCount)
	}
	if !j.LastError.Valid {
		t.Errorf("want LastError IS NOT NULL")
	}
	if j.LastError.String != "the error msg" {
		t.Errorf("want LastError=\"the error msg\" was: %q", j.LastError.String)
	}
}

func TestWorkerWorkRescuesPanic(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(context.Background(), c.pool)

	called := 0
	wm := WorkMap{
		"MyJob": func(j *Job) error {
			called++
			panic("the panic msg")
		},
	}
	w := NewWorker(c, wm)

	if err := c.Enqueue(context.Background(), &Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	w.WorkOne()
	if called != 1 {
		t.Errorf("want called=1 was: %d", called)
	}

	tx, err := c.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	j, err := findOneJob(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	if j.ErrorCount != 1 {
		t.Errorf("want ErrorCount=1 was %d", j.ErrorCount)
	}
	if !j.LastError.Valid {
		t.Errorf("want LastError IS NOT NULL")
	}
	if !strings.Contains(j.LastError.String, "the panic msg\n") {
		t.Errorf("want LastError contains \"the panic msg\\n\" was: %q", j.LastError.String)
	}
	// basic check if a stacktrace is there - not the stacktrace format itself
	if !strings.Contains(j.LastError.String, "worker.go:") {
		t.Errorf("want LastError contains \"worker.go:\" was: %q", j.LastError.String)
	}
	if !strings.Contains(j.LastError.String, "worker_test.go:") {
		t.Errorf("want LastError contains \"worker_test.go:\" was: %q", j.LastError.String)
	}
}

func TestWorkerWorkOneTypeNotInMap(t *testing.T) {
	c := openTestClient(t)
	defer truncateAndClose(context.Background(), c.pool)

	idleConns := c.pool.Stat().IdleConns()
	availConns := c.pool.Stat().AcquiredConns()

	wm := WorkMap{}
	w := NewWorker(c, wm)

	didWork := w.WorkOne()
	if didWork {
		t.Errorf("want didWork=false when no job was queued")
	}

	if err := c.Enqueue(context.Background(), &Job{Type: "MyJob"}); err != nil {
		t.Fatal(err)
	}

	didWork = w.WorkOne()
	if !didWork {
		t.Errorf("want didWork=true")
	}

	if idleConns+1 != c.pool.Stat().IdleConns() {
		t.Errorf("want idleConns euqual: before=%d  after=%d", idleConns, c.pool.Stat().IdleConns())
	}
	if availConns != c.pool.Stat().AcquiredConns() {
		t.Errorf("want availConns euqual: before=%d  after=%d", availConns, c.pool.Stat().AcquiredConns())
	}

	tx, err := c.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	j, err := findOneJob(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	if j.ErrorCount != 1 {
		t.Errorf("want ErrorCount=1 was %d", j.ErrorCount)
	}
	if !j.LastError.Valid {
		t.Fatal("want non-nil LastError")
	}
	if want := "unknown job type: \"MyJob\""; j.LastError.String != want {
		t.Errorf("want LastError=%q, got %q", want, j.LastError.String)
	}

}
