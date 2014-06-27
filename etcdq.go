package main

import (
	"github.com/opslabjpl/etcdq.git/etcdq"
	"log"
	"os"
)

func main() {
	config := &etcdq.WorkerConfig{
		AwsAccessKey: os.Getenv("AWS_ACCESS_KEY"),
		AwsSecretKey: os.Getenv("AWS_SECRET_KEY"),
		AwsRegion:    os.Getenv("AWS_REGION"),
		S3Bucket:     os.Getenv("S3_BUCKET"),
		Workspace:    os.Getenv("WORKSPACE"),
		EarthkitImg:  os.Getenv("EARTHKIT_IMG"),
		DataDir:      os.Getenv("DATA_DIR"),
	}
	if config.AwsAccessKey == "" {
		log.Fatal("Missing environment variable AWS_ACCESS_KEY")
	}
	if config.AwsSecretKey == "" {
		log.Fatal("Missing environment variable AWS_SECRET_KEY")
	}
	if config.AwsRegion == "" {
		log.Fatal("Missing environment variable AWS_REGION")
	}
	if config.S3Bucket == "" {
		log.Fatal("Missing environment variable S3_BUCKET")
	}
	if config.Workspace == "" {
		log.Fatal("Missing environment variable WORKSPACE")
	}
	if config.EarthkitImg == "" {
		log.Fatal("Missing environment variable EARTHKIT_IMG")
	}
	if config.DataDir == "" {
		config.DataDir = "/tmp"
	}

	worker, err := etcdq.NewWorker(config)
	if err != nil {
		log.Printf("Error: %s", err)
		return
	}
	worker.Start()
}
