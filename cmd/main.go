package main

import (
	"net/http"

	"github.com/emilioschepis/amanuense-go"
)

func main() {
	http.HandleFunc("/", amanuense.HandleMessage)
	if err := http.ListenAndServe(":3000", nil); err != nil {
		panic(err)
	}
}
