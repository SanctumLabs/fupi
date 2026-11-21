package scheduler

import (
	"log/slog"

	"github.com/robfig/cron/v3"
)

type JobScheduler struct {
	cron *cron.Cron
}

func NewJobScheduler() *JobScheduler {
	c := cron.New(
		cron.WithParser(
			cron.NewParser(
				cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
			),
		),
	)
	return &JobScheduler{cron: c}
}

func (s *JobScheduler) AddScheduler(schedule string, cmd func()) (cron.EntryID, error) {
	entryId, err := s.cron.AddFunc(schedule, cmd)
	if err != nil {
		slog.Error("Failed to add scheduler", "schedule", schedule, "err", err)
		return 0, err
	}
	return entryId, nil
}

func (s *JobScheduler) AddJob(schedule string, cmd cron.Job) (cron.EntryID, error) {
	entryId, err := s.cron.AddJob(schedule, cmd)
	if err != nil {
		slog.Error("Failed to add job", "schedule", schedule, "err", err)
		return 0, err
	}
	return entryId, nil
}

func (s *JobScheduler) Start() {
	slog.Info("Starting job scheduler")
	s.cron.Start()
}

func (s *JobScheduler) Stop() {
	slog.Info("Stopping job scheduler")
	s.cron.Stop()
}
