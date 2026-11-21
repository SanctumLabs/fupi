package scheduler

// ScheduledJob is a job that is scheduled to run at a given time
type ScheduledJob interface {
	Run()
}
