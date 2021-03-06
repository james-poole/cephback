package cmd

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

func httpHello(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello!") // send data to client side
}

func httpHealthz(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, health.Status()) // send data to client side
}

func httpServe() {

	go func() {
		logger.Infof("Listening on %s", httpListen)
		http.HandleFunc("/", httpHello)
		http.HandleFunc("/healthz", httpHealthz)
		http.Handle("/metrics", promhttp.Handler())
		err := http.ListenAndServe(httpListen, nil)
		if err != nil {
			logger.Fatal("ListenAndServe: ", err.Error())
		}
	}()
}
