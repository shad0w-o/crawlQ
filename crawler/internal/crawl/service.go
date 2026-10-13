package crawl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

type Job struct {
	ID        string    `json:"id"`
	Options   Options   `json:"options"`
	Progress  Progress  `json:"progress"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Service struct {
	store   *storage.Store
	mu      sync.RWMutex
	jobs    map[string]*Job
	cancels map[string]context.CancelFunc
}

func NewService(store *storage.Store) *Service {
	return &Service{
		store:   store,
		jobs:    make(map[string]*Job),
		cancels: make(map[string]context.CancelFunc),
	}
}

func (s *Service) Start(opts Options) (Job, error) {
	if s.store == nil {
		return Job{}, errors.New("crawl runner is not configured")
	}
	if len(opts.Seeds) == 0 {
		return Job{}, errors.New("at least one seed URL is required")
	}

	id, err := newID()
	if err != nil {
		return Job{}, err
	}
	now := time.Now()
	job := &Job{
		ID: id,
		Options: Options{
			Seeds:       append([]string(nil), opts.Seeds...),
			Recursive:   opts.Recursive,
			MaxDepth:    opts.MaxDepth,
			MaxPages:    opts.MaxPages,
			Concurrency: opts.Concurrency,
			Delay:       opts.Delay,
			UserAgent:   opts.UserAgent,
			CallbackURL: opts.CallbackURL,
		},
		Progress: Progress{
			Status:    StatusQueued,
			StartedAt: now,
			UpdatedAt: now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	ctx, cancel := context.WithCancel(context.Background())

	s.mu.Lock()
	s.jobs[id] = job
	s.cancels[id] = cancel
	s.mu.Unlock()

	go func(jobID string, crawlCtx context.Context) {
		s.update(jobID, func(j *Job) {
			j.Progress.Status = StatusCrawling
			j.Progress.StartedAt = time.Now()
		})

		// Propagate the job ID into the runner options so callbacks know which job produced pages.
		optsWithID := opts
		optsWithID.JobID = jobID

		final, err := Run(crawlCtx, s.store, optsWithID, func(progress Progress) {
			s.update(jobID, func(j *Job) {
				j.Progress = progress
				j.UpdatedAt = time.Now()
			})
		})

		s.update(jobID, func(j *Job) {
			j.Progress = final
			if err != nil && final.Status != StatusCanceled {
				j.Progress.Status = StatusFailed
				j.Progress.LastError = err.Error()
			}
			j.Progress.CompletedAt = time.Now()
			j.UpdatedAt = time.Now()
		})

		s.mu.Lock()
		delete(s.cancels, jobID)
		s.mu.Unlock()
	}(id, ctx)

	return s.snapshot(id), nil
}

func (s *Service) List() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, *cloneJob(job))
	}
	return jobs
}

func (s *Service) Get(id string) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *cloneJob(job), true
}

func (s *Service) Cancel(id string) bool {
	s.mu.RLock()
	cancel, ok := s.cancels[id]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	cancel()
	s.update(id, func(job *Job) {
		job.Progress.Status = StatusCanceled
		job.Progress.LastError = "cancelled by user"
		job.Progress.CompletedAt = time.Now()
	})
	return true
}

func (s *Service) snapshot(id string) Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if job, ok := s.jobs[id]; ok {
		return *cloneJob(job)
	}
	return Job{}
}

func (s *Service) update(id string, mutate func(*Job)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return
	}
	mutate(job)
	job.UpdatedAt = time.Now()
	if job.Progress.UpdatedAt.IsZero() {
		job.Progress.UpdatedAt = job.UpdatedAt
	}
	job.Progress.UpdatedAt = job.UpdatedAt
}

func cloneJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	copyJob := *job
	copyJob.Options.Seeds = append([]string(nil), job.Options.Seeds...)
	return &copyJob
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}