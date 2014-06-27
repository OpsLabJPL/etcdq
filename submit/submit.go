package main

import (
	"encoding/json"
	"fmt"
	"github.com/coreos/go-etcd/etcd"
	"strings"
	"time"
)

var template = `{
	"Container": {
		"Config": {
			"Image": "ubuntu",
			"Cmd": ["/bin/bash", "-c", "echo 'goodbye, world' > goodbye"]
		}
	},
	"Fileset": {
		"Input": {
			"Name": "%s",
			"Patterns": []
		},
		"Output": {
			"Name": "%s",
			"Patterns": []
		}
	},
	"States": [
		{
			"State": "QUEUED",
			"Step": "",
			"Date": "%s"
		}
	]
}`

func main() {
	// Initialize the etcd client
	machines := make([]string, 1)
	machines[0] = "http://127.0.0.1:4001"
	fmt.Println("syncing to Etcd cluster")
	client := etcd.NewClient(machines)
	// Make sure we can connect to the cluster using the local Etcd daemon
	if !client.SyncCluster() {
		fmt.Printf("Could not sync to cluster using host: %s\n", machines[0])
		return
	}
	timestamp, _ := json.Marshal(time.Now())
	tsStr := strings.Trim(string(timestamp), "\"")
	value := fmt.Sprintf(template, "hello", "hello_result_"+tsStr, tsStr)
	resp, err := client.CreateInOrder("/jobs", value, 0)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err)
	} else {
		fmt.Printf("Successfully submitted job: %s\n", resp.Node.Key)
	}
}
