package jobqueue

import (
	"errors"
	"time"

	"github.com/coreos/go-etcd/etcd"
	"github.com/kr/beanstalk"
)

// Default parameters
const (
	priority     = uint32(0)
	delay        = 5 * time.Second
	ttr          = 5 * time.Second
	timeout      = 10 * time.Hour
	reserveDelay = 5 * time.Second
	jobTTL       = 24 * time.Hour
)

// Client is for interacting with the job queue
type Client struct {
	beanConn *beanstalk.Conn
	etcd     *etcd.Client
	tubes    *tubes
}

// NewClient creates a new Client and initializes the beanstalk connection + tubes
func NewClient(bstalk string, e *etcd.Client) (*Client, error) {
	if e == nil {
		return nil, errors.New("missing etcd client")
	}

	conn, err := beanstalk.Dial("tcp", bstalk)
	if err != nil {
		return nil, err
	}

	client := &Client{
		beanConn: conn,
		etcd:     e,
		tubes:    newTubes(conn),
	}
	return client, nil
}

// AddTask creates a new task in the appropriate beanstalk queue
func (c *Client) AddTask(j *Job) (uint64, error) {
	if j == nil {
		return 0, errors.New("missing job")
	}

	ts := c.tubes.work
	if j.Action == "select-hypervisor" {
		ts = c.tubes.create
	}
	id, err := ts.Put(j.ID)
	return id, err
}

// DeleteTask removes a task from beanstalk by id
func (c *Client) DeleteTask(id uint64) error {
	return c.beanConn.Delete(id)
}

// NextCreateTask returns the next task from the create tube
func (c *Client) NextCreateTask() (*Task, error) {
	task, err := c.nextTask(c.tubes.create)
	return task, err
}

// NextWorkTask returns the next task from the work tube
func (c *Client) NextWorkTask() (*Task, error) {
	task, err := c.nextTask(c.tubes.work)
	return task, err
}

// nextTask returns the next task from a tubeSet and loads the Job and Guest
func (c *Client) nextTask(ts *tubeSet) (*Task, error) {
	id, body, err := ts.Reserve()
	if err != nil {
		return nil, err
	}

	// Build the Task object
	task := &Task{
		ID:     id,
		JobID:  body,
		client: c,
	}

	// Load the Job and Guest
	if err := task.RefreshJob(); err != nil {
		return task, err
	}
	if err := task.RefreshGuest(); err != nil {
		return task, err
	}

	return task, err
}

// AddJob creates a new job for a guest and adds a task for it
func (c *Client) AddJob(guestID, action string) (*Job, error) {
	job := c.NewJob()
	job.Guest = guestID
	job.Action = action
	if err := job.Save(jobTTL); err != nil {
		return nil, err
	}
	_, err := c.AddTask(job)
	return job, err
}
