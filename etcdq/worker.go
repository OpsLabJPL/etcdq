package etcdq

import (
	"encoding/json"
	"fmt"
	"github.com/coreos/go-etcd/etcd"
	"github.com/fsouza/go-dockerclient"
	"log"
	"path"
	"time"
)

func NewWorker(config *WorkerConfig) (worker *Worker, err error) {
	worker = &Worker{config: config}
	// Initialize the etcd client
	machines := make([]string, 1)
	machines[0] = "http://localhost:4001"
	log.Print("syncing to Etcd cluster")
	worker.etcd = etcd.NewClient(machines)
	// Make sure we can connect to the cluster using the local Etcd daemon
	if !worker.etcd.SyncCluster() {
		err = fmt.Errorf("Could not sync to cluster using host: %s", machines[0])
		worker = nil
		return
	}
	log.Print("connecting to Docker server")
	// Use local HTTP connection
	dockerEndpoint := "http://localhost:4243"
	worker.docker, err = docker.NewClient(dockerEndpoint)
	if err != nil {
		worker = nil
		return
	}
	log.Print("initializing parameters from EC2")
	err = worker.initializeFromEC2()
	if err != nil {
		log.Print("nvm... using defaults")
		worker.initializeDefault()
	}
	worker.Job = 0
	worker.Heartbeat = time.Now()
	worker.Stats = WorkerStats{}
	worker.Status = StatusAlive
	// Just make it the same thing as the InstanceId for now. Keeping a separate
	// variable for easier decoupling later.
	worker.Id = worker.InstanceId
	// "Register" the worker with Etcd by saving it.
	log.Print("registering worker with Etcd")
	err = worker.save()
	if err != nil {
		worker = nil
		return
	}
	worker.signals = make(chan Signal)
	worker.pollRate = 10 * time.Second
	worker.heartbeatRate = 1 * time.Minute
	return
}

func (worker *Worker) initializeFromEC2() (err error) {
	worker.Name, err = readEc2Parameter("meta-data/hostname")
	if err != nil {
		return err
	}
	worker.InstanceId, err = readEc2Parameter("meta-data/instance-id")
	if err != nil {
		return err
	}
	worker.PrivateIp, err = readEc2Parameter("meta-data/local-ipv4")
	if err != nil {
		return err
	}
	worker.PublicIp, err = readEc2Parameter("meta-data/public-ipv4")
	if err != nil {
		return err
	}
	return nil
}

func (worker *Worker) initializeDefault() {
	worker.Name = "worker"
	worker.InstanceId = "instance-id"
	worker.PrivateIp = "local-ipv4"
	worker.PublicIp = "public-ipv4"
}

// Saves the state of this worker to its Etcd key.  The key is based on the Worker.Id field.
func (worker *Worker) save() (err error) {
	log.Print("saving worker")
	value, err := json.Marshal(worker)
	if err != nil {
		log.Printf("ERROR: couldn't save worker: %s", err)
		return
	}
	key := path.Join(PathWorkers, worker.Id)
	_, err = worker.etcd.Set(key, string(value), 0)
	if err != nil {
		log.Printf("ERROR: couldn't save worker: %s", err)
		return
	}
	return
}

func (worker *Worker) Start() {
	log.Print("starting worker")
	pollTick := time.Tick(worker.pollRate)
	hbTick := time.Tick(worker.heartbeatRate)
	stayAlive := true
	// go func() {
	for stayAlive {
		select {
		case signal := <-worker.signals:
			log.Printf("got signal: %s", signal)
			if signal == SignalSave {
				worker.save()
			} else if signal == SignalJobDone {
				worker.Job = 0
				worker.currentJob = nil
				worker.save()
			} else {
				// For now, there are only SAVE, JOB_DONE, and STOP signals, so this is a STOP
				stayAlive = false
			}
		case now := <-hbTick:
			log.Print("heartbeat")
			worker.Heartbeat = now
			worker.save()
		case <-pollTick:
			log.Print("poll tick")
			if worker.currentJob == nil {
				worker.poll()
			}
		}
	}
	// }()
}

// The main polling logic for allocating and running jobs
func (worker *Worker) poll() {
	log.Print("polling for jobs")
	// List the directory sorted, but not recursive
	resp, err := worker.etcd.Get(PathJobs, true, false)
	if err != nil {
		log.Printf("ERROR: %s", err)
		if etcdErr, ok := err.(etcd.EtcdError); ok {
			// TODO: Handle etcd error ("key not found" = error code 100)
			if etcdErr.ErrorCode == 100 {
				// TODO
			}
		} else {
			// TODO: Handle possible network error
		}
		return
	}
	var job *Job
	for _, node := range resp.Node.Nodes {
		job, err = worker.tryAllocateJob(&node)
		if err != nil {
			return
		}
		if job != nil {
			break
		}
	}
	if job != nil {
		log.Printf("starting job: %s", job.id)
		job.worker = worker
		worker.currentJob = job
		go job.start()
	}
}

// Attempts to allocate a job from the work queue.  This method will return a Job
// if successful, and nil otherwise (probably with an error).
func (worker *Worker) tryAllocateJob(node *etcd.Node) (job *Job, err error) {
	j := &Job{}
	err = json.Unmarshal([]byte(node.Value), j)
	if err != nil {
		// TODO: Probably just log this and move on?
		log.Printf("ERROR: %s", err)
		return
	}
	job = j
	// Current state is in last position
	state := job.States[len(job.States)-1]
	if state.State != JobStateQueued {
		// Job is already allocated / in progress
		job = nil
		return
	}
	// Attempt to allocate this job atomically
	job.setState(JobStateStarted, "Allocated")
	job.Owner = worker.Id
	job.id = path.Base(node.Key)
	b, err := json.Marshal(job)
	if err != nil {
		// TODO
		log.Printf("ERROR: %s", err)
		job = nil
		return
	}
	_, err = worker.etcd.CompareAndSwap(node.Key, string(b), 0, "", node.ModifiedIndex)
	if err != nil {
		log.Printf("ERROR: %s", err)
		job = nil
		if etcdErr, ok := err.(etcd.EtcdError); ok {
			// Atomic CAS error code is 101 on failure
			if etcdErr.ErrorCode == 101 {
				err = nil
				return
			}
			// TODO: How to handle other Etcd errors?
		}
		// TODO: Handle possible network errors?
		return
	}
	return
}
