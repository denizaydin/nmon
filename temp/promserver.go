package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var jobsInQueue = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "jobs_in_queue",
		Help: "Current number of jobs in the queue",
	},
)

func init() {
	prometheus.MustRegister(jobsInQueue)
}
func enqueueJob(job Job) {
	queue.Add(job)
	jobsInQueue.Inc()
}

func runNextJob() {
	job := queue.Dequeue()
	jobsInQueue.Dec()

	job.Run()
}

func main() {
	http.Handle("/metrics", promhttp.Handler())
	panic(http.ListenAndServe("localhost:8080", nil))
}
