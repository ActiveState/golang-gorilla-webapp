package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	setupFixtures()
	os.Exit(m.Run())
}

var testDB *sql.DB

func setupFixtures() {
	os.Setenv("HASHTEXT_DB", "hashtext_test")
	// This has the gross side effect of also setting the global db var in
	// main.go which in turn is used in handlers.go. In a real application,
	// we'd want to wrap up our handlers in a struct that contained a *sql.DB,
	// and possible even go further and create these handlers using dependency
	// injection.
	db = openDB()
	execWithCheck(db, `DELETE FROM "user"`)
	execWithCheck(db, `DELETE FROM "hash_text"`)
	populateTables(db)
}

type User struct {
	name   string
	credit int
}

func populateTables(db *sql.DB) {
	users := []User{
		{"Jane", 1000000},
		{"Xiomara", 1000000},
		{"Petra", 0}, // Petra has no credit and cannot use the service
	}

	for _, u := range users {
		execWithCheck(db, `INSERT INTO "user" (user_id, name, credit) VALUES ($1, $2, $3)`,
			sha256String(u.name), u.name, u.credit)
	}
}

func execWithCheck(db *sql.DB, s string, args ...interface{}) {
	_, err := db.Exec(s, args...)
	if err != nil {
		log.Fatal("** Error executing SQL - " + err.Error() + ": " + s)
	}
}

func TestUserIsAuthorized(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.com/", nil)
	assert.False(t, userIsAuthorized(r), "returns false when there is no X-HashText-User-ID header")

	r.Header.Set("X-HashText-User-ID", "")
	assert.False(t, userIsAuthorized(r), "returns false when the X-HashText-User-ID header is empty")

	r.Header.Set("X-HashText-User-ID", "0")
	assert.False(t, userIsAuthorized(r), "returns false when the X-HashText-User-ID header is 0")

	r.Header.Set("X-HashText-User-ID", "foo")
	assert.False(t, userIsAuthorized(r), "returns false when the X-HashText-User-ID header is foo")

	r.Header.Set("X-HashText-User-ID", sha256String("Jane"))
	assert.True(t, userIsAuthorized(r), "returns true when the X-HashText-User-ID header is the SHA256 hash for Jane")
}

func TestUserHasCredit(t *testing.T) {
	assert.True(t, userHasCredit(sha256String("Jane")), "Jane has credit")
	assert.False(t, userHasCredit(sha256String("Petra")), "Petra does not have credit")
}

func testUserHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/user/foo", nil)
	resp, body := fakeRequest(req, userHandler)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "returned 404 for unknown user")
	assert.Equal(t, []byte{}, body, "no body in response")

	userID := sha256String("Jane")
	req = httptest.NewRequest("GET", fmt.Sprintf("http://example.com/user/%s", userID), nil)
	resp, body = fakeRequest(req, userHandler)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "returned 200 for user who exists")
	assert.Equal(t, "application/json; charset=UTF-8", resp.Header.Get("Content-Type"), "got expected Content-Type in response")

	var u userDocument
	err := json.Unmarshal(body, &u)
	assert.Nil(t, err, "no error unmarshalling response body")
	assert.Equal(t, userDocument{UserID: userID, Name: "Jane", Credit: 1000000}, "got user data for Jane")
}

func TestTextHandler(t *testing.T) {
	text := "test text handler"
	j, err := json.Marshal(map[string]string{"text": text})
	assert.Nil(t, err, "no error marshalling textRequest")

	req := httptest.NewRequest("POST", "http://example.com/text", bytes.NewBuffer(j))
	userID := sha256String("Jane")
	req.Header.Set("X-HashText-User-ID", userID)
	resp, body := fakeRequest(req, textHandler)

	assert.Equal(t, http.StatusOK, resp.StatusCode, "returned 200 for user who exists")
	assert.Equal(t, "application/json; charset=UTF-8", resp.Header.Get("Content-Type"), "got expected Content-Type in response")

	var hd hashDocument
	err = json.Unmarshal(body, &hd)
	assert.Nil(t, err, "no error unmarshalling response body")
	assert.Equal(t, hashDocument{Hash: sha256String(text)}, hd, "got expected reponse after posting text")

	row := db.QueryRow(`SELECT credit FROM "user" WHERE user_id = $1`, userID)
	var credit int
	err = row.Scan(&credit)
	assert.Nil(t, err, "no error looking up credit for Jane")
	assert.Equal(t, 999999, credit, "credit was debited after inserting text")

	row = db.QueryRow(`SELECT hash, text FROM hash_text WHERE text = $1`, text)
	var hash string
	var dbText string
	err = row.Scan(&hash, &dbText)
	assert.Nil(t, err, "no error looking up hash_text")
	assert.Equal(t, sha256String(text), hash, "stored expected hash for text in database")
	assert.Equal(t, text, dbText, "stored text as-is in database")

	req = httptest.NewRequest("POST", "http://example.com/text", bytes.NewBuffer(j))
	userID = sha256String("Petra")
	req.Header.Set("X-HashText-User-ID", userID)
	resp, body = fakeRequest(req, textHandler)

	assert.Equal(t, http.StatusPaymentRequired, resp.StatusCode, "returned 402 for user without credit")
	assert.Equal(t, "text/plain; charset=UTF-8", resp.Header.Get("Content-Type"), "got expected Content-Type in response")
	assert.Equal(t, "You are out of credit. Please pay us more money.", string(body), "got expected error message in body")
}

func TestTextHashHandler(t *testing.T) {
	// The textHashHandler uses mux.Vars(), which in turn requires that we
	// make the router, which in turn requires that we authenticate ourselves
	// in the request.
	text := "test text hash handler"
	hash := sha256String(text)

	_, err := db.Exec("INSERT INTO hash_text (hash, text) VALUES ($1, $2)", hash, text)
	assert.Nil(t, err, "inserted text and hash")

	req := httptest.NewRequest("GET", fmt.Sprintf("http://example.com/text/%s", hash), nil)
	userID := sha256String("Jane")
	req.Header.Set("X-HashText-User-ID", userID)
	resp, body := fakeRequest(req, func(w http.ResponseWriter, r *http.Request) { makeRouter().ServeHTTP(w, r) })

	assert.Equal(t, http.StatusOK, resp.StatusCode, "returned 200 for hash which exists")
	assert.Equal(t, "application/json; charset=UTF-8", resp.Header.Get("Content-Type"), "got expected Content-Type in response")

	var td textDocument
	err = json.Unmarshal(body, &td)
	assert.Equal(t, textDocument{Text: text}, td, "got text for hash")

	req = httptest.NewRequest("GET", "http://example.com/text/does-not-exist", nil)
	req.Header.Set("X-HashText-User-ID", userID)
	resp, body = fakeRequest(req, func(w http.ResponseWriter, r *http.Request) { makeRouter().ServeHTTP(w, r) })

	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "returned 404 for hash which does not exist")
}

func fakeRequest(
	req *http.Request,
	handler func(w http.ResponseWriter, r *http.Request),
) (*http.Response, []byte) {

	w := httptest.NewRecorder()

	handler(w, req)
	resp := w.Result()
	respBody, _ := ioutil.ReadAll(resp.Body)

	return resp, respBody
}
