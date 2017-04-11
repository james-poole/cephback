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
	fmt.Fprintf(w, "OK") // send data to client side
}

func httpMetrics(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "some metrics here...") // send data to client side
}

func httpServe() {

	go func() {
		logger.Infof("Serving on %s", httpListen)
		http.HandleFunc("/", httpHello)
		http.HandleFunc("/healthz", httpHealthz)
		//		http.HandleFunc("/metrics", httpMetrics)
		http.Handle("/metrics", promhttp.Handler())
		err := http.ListenAndServe(httpListen, nil)
		if err != nil {
			logger.Fatal("ListenAndServe: ", err)
		}
	}()
}
