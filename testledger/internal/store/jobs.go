// Asynchronous job records. A server marks jobs left running by a prior
// process as interrupted on startup.
package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/danielcsee/sciterm/testledger/internal/model"
	_ "modernc.org/sqlite"
)

func (s *Store) CreateJob(ctx context.Context, job model.AsyncJob) error {
	arguments, _ := json.Marshal(job.Arguments)
	_, err := s.db.ExecContext(ctx, `INSERT INTO async_jobs(id,kind,status,arguments_json,created_at) VALUES(?,?,?,?,?)`,
		job.ID, job.Kind, job.Status, string(arguments), job.CreatedAt)
	return err
}

func (s *Store) StartJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE async_jobs SET status='running',started_at=? WHERE id=? AND status='queued'`, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) FinishJob(ctx context.Context, id, status, runID, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE async_jobs SET status=?,finished_at=?,result_run_id=?,error=? WHERE id=?`,
		status, time.Now().UTC().Format(time.RFC3339Nano), nullString(runID), nullString(message), id)
	return err
}

func (s *Store) GetJob(ctx context.Context, id string) (model.AsyncJob, error) {
	var job model.AsyncJob
	var arguments, started, finished, runID, message string
	err := s.db.QueryRowContext(ctx, `SELECT id,kind,status,arguments_json,created_at,COALESCE(started_at,''),COALESCE(finished_at,''),COALESCE(result_run_id,''),COALESCE(error,'')
FROM async_jobs WHERE id=?`, id).Scan(&job.ID, &job.Kind, &job.Status, &arguments, &job.CreatedAt, &started, &finished, &runID, &message)
	if err != nil {
		return job, err
	}
	_ = json.Unmarshal([]byte(arguments), &job.Arguments)
	job.StartedAt, job.FinishedAt, job.ResultRunID, job.Error = started, finished, runID, message
	return job, nil
}

func (s *Store) ActiveJobs(ctx context.Context) ([]model.AsyncJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM async_jobs WHERE status IN ('queued','running') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	jobs := []model.AsyncJob{}
	for _, id := range ids {
		job, err := s.GetJob(ctx, id)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *Store) InterruptActiveJobs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE async_jobs SET status='interrupted',finished_at=?,error='MCP server restarted before completion'
WHERE status IN ('queued','running')`, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
