package main

import "github.com/gorilla/mux"

func makeRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/user/me", wrapHandler(userHandler)).Methods("GET")
	r.HandleFunc("/text", wrapHandler(textHandler)).Methods("POST")
	r.HandleFunc("/text/{hash}", wrapHandler(textHashHandler)).Methods("GET")
	return r
}
