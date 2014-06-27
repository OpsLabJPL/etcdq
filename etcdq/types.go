package etcdq

import (
	"github.com/coreos/go-etcd/etcd"
	"github.com/fsouza/go-dockerclient"
	"time"
)

type Signal string

type WorkerStats struct {
	Cpu  float32
	Mem  float32
	MemX int
}

type WorkerStatus string

type WorkerConfig struct {
	AwsAccessKey string
	AwsSecretKey string
	AwsRegion    string
	S3Bucket     string
	Workspace    string
	EarthkitImg  string
	DataDir      string
}

type Worker struct {
	// The following are exported fields matching a worker's serialized form
	Name       string
	InstanceId string
	PrivateIp  string
	PublicIp   string
	Job        int64
	Heartbeat  time.Time
	Stats      WorkerStats
	Status     WorkerStatus
	Id         string
	// The following are unexported private state variables
	etcd          *etcd.Client
	docker        *docker.Client
	pollRate      time.Duration
	heartbeatRate time.Duration
	signals       chan Signal
	currentJob    *Job
	config        *WorkerConfig
}

type Fileset struct {
	Name     string
	Patterns []string
}

type JobFileset struct {
	Input  *Fileset
	Output *Fileset
}

type JobState string

type State struct {
	State JobState
	Step  string
	Date  time.Time
}

type JobResult struct {
	Status string
	Info   string
}

type Job struct {
	Container *docker.Container
	Fileset   *JobFileset
	Owner     string
	States    []State
	Result    *JobResult
	// The following are unexported private state variables
	id     string
	worker *Worker
}
