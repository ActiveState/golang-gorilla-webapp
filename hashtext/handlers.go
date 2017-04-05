package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

func wrapHandler(
	handler func(w http.ResponseWriter, r *http.Request),
) func(w http.ResponseWriter, r *http.Request) {

	h := func(w http.ResponseWriter, r *http.Request) {
		if !userIsAuthorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler(w, r)
	}
	return h
}

func userIsAuthorized(r *http.Request) bool {
	userID := r.Header.Get("X-HashText-User-ID")
	if userID == "" {
		return false
	}

	var found bool
	err := db.QueryRow(`SELECT 1 FROM "user" WHERE user_id = $1`, userID).Scan(&found)
	switch {
	case err == sql.ErrNoRows:
		return false
	case err != nil:
		log.Printf("Query to look up user failed: %v", err)
		return false
	}

	return found
}

type userDocument struct {
	UserID string `json:user_id`
	Name   string
	Credit int
}

func userHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-HashText-User-ID")

	row := db.QueryRow(`SELECT name, credit FROM "user" WHERE user_id = $1`, userID)

	var name string
	var credit int
	err := row.Scan(&name, &credit)
	switch {
	case err == sql.ErrNoRows:
		w.WriteHeader(http.StatusNotFound)
		return
	case err != nil:
		log.Printf("Query to look up user failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, userDocument{UserID: userID, Name: name, Credit: credit})
}

type textDocument struct {
	Text string "json:text"
}

type hashDocument struct {
	Hash string "json:hash"
}

func textHandler(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-HashText-User-ID")
	if !userHasCredit(userID) {
		sendErrorMessage(w, "You are out of credit. Please pay us more money.", http.StatusPaymentRequired)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("Failed to read the request body: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var td textDocument
	if err := json.Unmarshal(body, &td); err != nil {
		sendErrorMessage(w, "Could not decode the request body as JSON", http.StatusBadRequest)
		return
	}

	// This will work with an empty string, for some value of work. If we
	// wanted to make this a bit smarter, we'd check the length of the text
	// submitted and return an error if it's empty.
	//
	// In a production application we might want to do the insert in a
	// goroutine, but this makes testing much more complicated.
	hash := sha256String(td.Text)
	insertText(td.Text, hash, userID)
	sendJSONResponse(w, hashDocument{Hash: hash})
}

func sha256String(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func userHasCredit(userID string) bool {
	row := db.QueryRow(`SELECT credit FROM "user" WHERE user_id = $1`, userID)

	var credit int
	err := row.Scan(&credit)
	if err != nil {
		log.Printf("Query to look up user failed: %v", err)
		// We might want to return a 500 here but this code is getting
		// complicated enough ...
		return false
	}

	return credit > 0
}

func insertText(text, hash, userID string) {
	_, err := db.Exec("INSERT INTO hash_text (hash, text) VALUES ($1, $2) ON CONFLICT DO NOTHING", hash, text)
	if err != nil {
		log.Printf("Failed to insert text with hash = %s: %v", hash, err)
		return
	}

	_, err = db.Exec(`UPDATE "user" SET credit = GREATEST(0, credit - 1) WHERE user_id = $1`, userID)
	if err != nil {
		log.Printf("Failed to debit user with user_id = %s: %v", userID, err)
		return
	}
}

func textHashHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	row := db.QueryRow(`SELECT text FROM hash_text WHERE hash = $1`, vars["hash"])

	var text string
	err := row.Scan(&text)
	switch {
	case err == sql.ErrNoRows:
		w.WriteHeader(http.StatusNotFound)
		return
	case err != nil:
		log.Printf("Query to look up text by hash failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, textDocument{Text: text})
}

func sendErrorMessage(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(status)
	io.WriteString(w, msg)
}

func sendJSONResponse(w http.ResponseWriter, data interface{}) {
	body, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to encode a JSON response: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(body)
	if err != nil {
		log.Printf("Failed to write the response body: %v", err)
		return
	}
}
