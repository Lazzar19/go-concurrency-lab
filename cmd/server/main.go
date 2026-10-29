package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	addr := listenAddr(os.Getenv("PORT"))
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, newMux()); err != nil {
		log.Fatal(err)
	}
}

func listenAddr(port string) string {
	if port == "" {
		port = "8080"
	}
	return "0.0.0.0:" + port
}
