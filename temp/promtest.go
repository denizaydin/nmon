package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func recordMetrics(i uint32) {
	go func() {
		for {
			pingResulut.WithLabelValues("localhost").Set(float64(i))
			time.Sleep(1 * time.Second)
		}
	}()
}
func init() {
	prometheus.MustRegister(pingResulut)
}

var (
	pingResulut = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "nmon",
		Subsystem: "ping",
		Name:      "test",
		Help:      "",
	},
		[]string{
			// Which user has requested the operation?
			"client",
		})
)

func main() {
	go func() {
		for {
			i := rand.Uint32()
			recordMetrics(i)
			fmt.Print(i, "\n")
			time.Sleep(time.Millisecond * 200)
		}
	}()
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe("localhost:2112", nil)

}
