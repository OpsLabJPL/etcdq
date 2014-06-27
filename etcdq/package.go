package etcdq

import (
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"io/ioutil"
	"net"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	StatusAlive = WorkerStatus("ALIVE")
	StatusDead  = WorkerStatus("DEAD")

	JobStateQueued   = JobState("QUEUED")
	JobStateStarted  = JobState("STARTED")
	JobStateFinished = JobState("FINISHED")

	PathWorkers = "/workers"
	PathJobs    = "/jobs"

	SignalStop    = Signal("STOP")
	SignalSave    = Signal("SAVE")
	SignalJobDone = Signal("JOB_DONE")
	SignalOK      = Signal("OK")
)

var (
	JobRate = 5 * time.Second
)

func JobKey(id string) string {
	return path.Join(PathJobs, id)
}

func WorkerKey(id string) string {
	return path.Join(PathWorkers, id)
}

func dialTimeout(network, addr string) (net.Conn, error) {
	return net.DialTimeout(network, addr, 2*time.Second)
}

func readEc2Parameter(parameter string) (value string, err error) {
	transport := http.Transport{
		Dial: dialTimeout,
	}
	client := http.Client{
		Transport: &transport,
	}

	url := fmt.Sprintf("http://169.254.169.254/latest/%s", parameter)
	resp, err := client.Get(url)
	if err != nil {
		return
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}
	value = string(b)
	return
}

func parsePullImageOptions(image string) (opts docker.PullImageOptions) {
	var host, repo, tag string
	// First split the image name into potential host and repo:tag components
	split := strings.SplitN(image, "/", 2)
	// Now split the tag from the repo
	if len(split) == 1 {
		split = strings.Split(split[0], ":")
	} else {
		host = split[0]
		split = strings.Split(split[1], ":")
	}
	repo = split[0]
	if len(split) > 1 {
		tag = split[1]
	} else {
		tag = "latest"
	}
	// IMPORTANT: Currently the Registry field/argument isn't used in Docker's "create image" (Pull) API.
	// You specify the registry hostname as part of the image name. So here we join the repo with the host.
	opts.Registry = ""
	opts.Repository = path.Join(host, repo)
	opts.Tag = tag
	opts.OutputStream = ioutil.Discard
	return
}
