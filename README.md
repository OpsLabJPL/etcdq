# Etcd Job Queue

A distributed job queue built on Etcd for running Docker jobs.

## Workers

A list of workers in the pool can be found at the following entry:

* /v2/keys/workers

### Schema

```
{
	"Name": "name according to etcd",
	"InstanceId": "instance-id",
	"PrivateIp": "private IP address",
	"PublicIp": "128.123.123.123"
	"Job": 14
	"Heartbeat": "timestamp"
	"Stats": {
		"Cpu": 20.5,
		"Mem": 10
	},
	"Status": "ALIVE | DEAD"
}
```

The `Job` field will be `nil` or an empty string or not present if the instance is idle.  Otherwise it will be
set to the ID string (which is an auto-incremented number, as a string) of the job it is processing.

The `Heartbeat` should be updated every few minutes by a worker to ensure that a worker is still alive.
Workers presumed dead (after some timeout) will be harvested from the pool and their jobs.  Once a worker
is harvested, the instance will be shut down (or rebooted) and the worker entry's `Status` will be marked
as `DEAD`.

The `Stats` field will contain useful statistics about the instance.  This is not necessary but would
probably be nice to have.

## Etcd Job Queue

Currently just a single etcd directory for the job queue:

* /v2/keys/jobs

It might be desireable to break this queue up into three queues (seen below) to make querying particular state more efficient.
With a single queue, you would list all jobs and filter the results based on the state.  With three queues a client
could just query all jobs in the particular queue.  However, getting a list of all jobs would then require listing
all three directories and merging the results.

* /v2/keys/new
* /v2/keys/running
* /v2/keys/finished

### Schema

The following JSON structure can fully represent a job and its state / results.  Notes follow.

```
{
	"Container": {
		"Config": ""
		// Other stats from docker
	},
	"Fileset": {
		"Input": {
			"Name": "foo",
			"Patterns": ["*.py", "bin/*"]
		},
		"Output": {
			"Name": "foo_result",
			"Patterns": ["**/*.dat"]
		}
	},
	"Owner": "worker-id",
	"States": [
		{
			"State": "QUEUED | STARTED | FINISHED",
			"Step": "some sort of status, or empty",
			"Date": "timestamp"
		}
	],
	"Result": {
		"Status": "SUCCESS | FAILURE",
		"Info": "more detailed info here"
	}
}
```

The `Container` object will be the same returned by Docker's "describe containers" API command. Before being run,
however, the `Container` object will only contain the `Config` field describing the Docker configuration for
creating the container.

The `Fileset` object contains an input and an output fileset description.  The input fileset will be pulled to the machine
performing a job before running the job.  The fileset will be pulled using the given patterns as filters (which are optional).
Once the job completes, the resulting data will be pushed back as the given output fileset name, only including files that
match the given patterns (which are optional).

The `Owner` field will be `nil` or empty string or not present if the state of the job is `QUEUED`.  Once running, this field
will contain the worker ID string of the worker as found in the workers directory.  `Owner` can change over time.  For example,
if an instance starts a job and then dies (for some reason), eventually the job will time out and another instance
will take over and update the `Owner` field.

The `States` field is a list of state objects.  A job will start with a list containing the single stated, `QUEUED`,
with the timestamp of when it was queued.  The last element in the list is the most recent state.  The `Step` field
of a `State` object can contain any string, really, and may change over time.  It is just there to elucidate the
current state.

The `Result` object simply indicated the result of the job, whether `SUCCESS` or `FAILURE`, as well as detailed
information about the meaning of the result.

## Process

### Submit a new job

1. POST new job schema to etcd: /v2/keys/jobs
2. The new job must have the `Container.Config`, `Fileset`, and `States` fields. `States` should be a single element array with the initial `QUEUED` state object: `"States":[{"State":"QUEUED","Step":"","Date":"timestamp"}]`

### Worker Job Lifecycle

1. List jobs in job queue
2. Find first entry that is in state `QUEUED`
3. Perform atomic CAS, setting the stat to `STARTED`
    * If this fails, another worker got the job.  Go to step 2.
4. Create the container (do not start it)
5. Pull the data
6. Start the container
7. Wait for container to stop
8. Push the data
9. Change state to `FINISHED` and update `Result`

### Harvest workers

This process is to be performed "randomly" by various workers every so often:

1. List all workers
2. Make sure their heartbeat is within some timeout
3. If heartbeat within timeout, nothing to do
4. Otherwise, set worker status to `DEAD` and shut down (or restart) the instance. Also need to remove dead instance's entry from etcd discovery endpoint
5. If it has an assigned job:
    1. If it is not finished, reset the state of the job to `QUEUED`
    2. If it is finished, leave it alone
