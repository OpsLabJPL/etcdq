package etcdq

import (
	"encoding/json"
	"fmt"
	"github.com/fsouza/go-dockerclient"
	"log"
	"path"
	"strings"
	"time"
)

func (job *Job) setState(jobState JobState, step string) {
	state := State{jobState, step, time.Now()}
	job.States = append(job.States, state)
}

// Starts a job. This should probably be called as a new goroutine.
func (job *Job) start() {
	var err error
	err = job.pullImages()
	if err != nil {
		job.finish(&JobResult{"FAILURE", err.Error()})
		return
	}
	err = job.pullData()
	if err != nil {
		job.finish(&JobResult{"FAILURE", err.Error()})
		return
	}
	err = job.runContainer()
	if err != nil {
		job.finish(&JobResult{"FAILURE", err.Error()})
		return
	}
	err = job.pushData()
	if err != nil {
		job.finish(&JobResult{"FAILURE", err.Error()})
		return
	}
	job.finish(&JobResult{"SUCCESS", ""})
}

func (job *Job) pullImages() (err error) {
	// Initialize a set of image names that need to exist before doing anything
	imagesToPull := make(map[string]docker.PullImageOptions)
	// Add earthkit-cli image
	opts := parsePullImageOptions(job.worker.config.EarthkitImg)
	key := fmt.Sprintf("%s:%s", opts.Repository, opts.Tag)
	imagesToPull[key] = opts
	// Add job-specified image
	opts = parsePullImageOptions(job.Container.Config.Image)
	key = fmt.Sprintf("%s:%s", opts.Repository, opts.Tag)
	imagesToPull[key] = opts
	// Get a list of all the images that currently exist locally
	existingImages, err := job.worker.docker.ListImages(false)
	if err != nil {
		return
	}
	// Remove existing images from our set of images to pull
	for _, apiImage := range existingImages {
		for _, imageName := range apiImage.RepoTags {
			if _, ok := imagesToPull[imageName]; ok {
				job.log("Image [%s] already exists locally", imageName)
				delete(imagesToPull, imageName)
			}
		}
	}
	// Now pull any images that haven't been pulled
	for imageName, opts := range imagesToPull {
		job.log("Image [%s] doesn't exist. Pulling...", imageName)
		job.setState(JobStateStarted, "Pulling docker image: "+imageName)
		err = job.worker.docker.PullImage(opts, docker.AuthConfiguration{})
		if err != nil {
			return
		}
	}
	return
}

func (job *Job) pullData() (err error) {
	job.log("pulling")
	fileset := job.Fileset.Input.Name
	vol := "/workspaces"
	vols := make(map[string]struct{})
	vols[vol] = struct{}{}
	workerCfg := job.worker.config
	cmd := "/bin/earthkit-cli -aws_access_key %s -aws_secret_key %s -aws_region %s -s3_bucket %s clone %s"
	if len(fileset) > 0 {
		cmd = cmd + " " + fileset
	}
	cmd = fmt.Sprintf(cmd, workerCfg.AwsAccessKey, workerCfg.AwsSecretKey, workerCfg.AwsRegion, workerCfg.S3Bucket, workerCfg.Workspace)
	config := &docker.Config{
		Image:      job.worker.config.EarthkitImg,
		WorkingDir: vol,
		Cmd:        strings.Split(cmd, " "),
		Volumes:    vols,
	}
	job.log("creating container")
	containerOpts := docker.CreateContainerOptions{Config: config}
	container, err := job.worker.docker.CreateContainer(containerOpts)
	if err != nil {
		return
	}
	binds := make([]string, 1)
	dataDir := job.worker.config.DataDir
	binds[0] = fmt.Sprintf("%s%s:%s:rw", dataDir, vol, vol)
	hostConfig := &docker.HostConfig{Binds: binds}
	job.log("starting container with id %s", container.ID)
	err = job.worker.docker.StartContainer(container.ID, hostConfig)
	if err != nil {
		return
	}
	friendlyFileset := fileset
	if len(fileset) == 0 {
		friendlyFileset = "latest"
	}
	job.setState(JobStateStarted, "Pulling fileset: "+friendlyFileset)
	job.save()
	job.log("waiting for container")
	code, err := job.worker.docker.WaitContainer(container.ID)
	if err == nil {
		return
	}
	if code != 0 {
		err = fmt.Errorf("Error return code from container: %d", code)
		return
	}
	job.log("removing container")
	err = job.worker.docker.RemoveContainer(docker.RemoveContainerOptions{ID: container.ID, RemoveVolumes: false})
	if err != nil {
		job.log("error: %s", err)
	}
	return
}

func (job *Job) runContainer() (err error) {
	job.log("running")
	workspace := job.worker.config.Workspace
	vol := "/workspaces"
	vols := make(map[string]struct{})
	vols[vol] = struct{}{}
	// Join the client-specified relative working dir with the volume/workspace path.
	job.Container.Config.WorkingDir = path.Join(vol, workspace, job.Container.Config.WorkingDir)
	job.Container.Config.Volumes = vols
	containerOpts := docker.CreateContainerOptions{Config: job.Container.Config}
	job.log("creating container")
	container, err := job.worker.docker.CreateContainer(containerOpts)
	if err != nil {
		return
	}
	// Update the Job state with the real container information. We have to backup and use the existing known
	// config because the container.Config field is nil for the Container struct returned by CreateContainer.
	config := job.Container.Config
	job.Container = container
	job.Container.Config = config
	binds := make([]string, 1)
	dataDir := job.worker.config.DataDir
	binds[0] = fmt.Sprintf("%s:%s:rw", path.Join(dataDir, vol), vol)
	hostConfig := &docker.HostConfig{Binds: binds}
	job.log("starting container with id %s", container.ID)
	err = job.worker.docker.StartContainer(container.ID, hostConfig)
	if err != nil {
		return
	}
	job.setState(JobStateStarted, "RUNNING")
	job.save()
	job.log("waiting for container")
	code, err := job.worker.docker.WaitContainer(container.ID)
	if err == nil {
		return
	}
	if code != 0 {
		err = fmt.Errorf("Error return code from container: %d", code)
		return
	}
	// TODO:  For now I am leaving this commented out.  We probably need to address disk space issues in the future.
	// But it would be good for users to retrieve logs from completed jobs.
	// job.log("removing container")
	// err = job.worker.docker.RemoveContainer(docker.RemoveContainerOptions{ID: container.ID, RemoveVolumes: false})
	return
}

func (job *Job) pushData() (err error) {
	vol := "/workspaces"
	vols := make(map[string]struct{})
	vols[vol] = struct{}{}
	workerCfg := job.worker.config
	cmd := "/bin/earthkit-cli -aws_access_key %s -aws_secret_key %s -aws_region %s -s3_bucket %s push %s"
	if job.Fileset.Output == nil || len(job.Fileset.Output.Name) == 0 {
		job.log("no output fileset specified -- not pushing")
		return
	}
	job.log("pushing")
	fileset := job.Fileset.Output.Name
	cmd = fmt.Sprintf(cmd, workerCfg.AwsAccessKey, workerCfg.AwsSecretKey, workerCfg.AwsRegion, workerCfg.S3Bucket, fileset)
	config := &docker.Config{
		Image:      job.worker.config.EarthkitImg,
		WorkingDir: path.Join(vol, workerCfg.Workspace),
		Cmd:        strings.Split(cmd, " "),
		Volumes:    vols,
	}
	containerOpts := docker.CreateContainerOptions{Config: config}
	job.log("creating container")
	container, err := job.worker.docker.CreateContainer(containerOpts)
	if err != nil {
		return
	}
	binds := make([]string, 1)
	dataDir := job.worker.config.DataDir
	binds[0] = fmt.Sprintf("%s%s:%s:rw", dataDir, vol, vol)
	hostConfig := &docker.HostConfig{Binds: binds}
	job.log("starting container with id %s", container.ID)
	err = job.worker.docker.StartContainer(container.ID, hostConfig)
	if err != nil {
		return
	}
	job.setState(JobStateStarted, "Pushing fileset: "+fileset)
	job.save()
	job.log("waiting for container")
	code, err := job.worker.docker.WaitContainer(container.ID)
	if err == nil {
		return
	}
	if code != 0 {
		err = fmt.Errorf("Error return code from container: %d", code)
		return
	}
	job.log("removing container")
	err = job.worker.docker.RemoveContainer(docker.RemoveContainerOptions{ID: container.ID, RemoveVolumes: false})
	if err != nil {
		job.log("error: %s", err)
	}
	return
}

// Finishes the job, setting its state to FINISHED and updating its result (which could
// be SUCCESS or FAILURE).
func (job *Job) finish(result *JobResult) {
	job.log("finishing with result: %s", result)
	job.Result = result
	job.setState(JobStateFinished, "")
	job.save()
	job.worker.signals <- SignalJobDone
}

func (job *Job) save() {
	job.log("saving")
	value, err := json.Marshal(job)
	if err != nil {
		job.log("ERROR: couldn't save: %s", err)
		return
	}
	key := JobKey(job.id)
	_, err = job.worker.etcd.Set(key, string(value), 0)
	if err != nil {
		job.log("ERROR: couldn't save: %s", err)
		return
	}
	return
}

func (job *Job) log(format string, v ...interface{}) {
	s := fmt.Sprintf(format, v...)
	log.Printf("job[%s] - %s", job.id, s)
}
