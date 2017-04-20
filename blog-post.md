Go is a great language for web applications. The Go core library has excellent
HTTP support, and the language's support for asynchronous execution lends
itself well to high performance web applications.

While you could start a webapp from scratch with the
core [`net/http`](http://docs.activestate.com/activego/1.8/pkg/http.1.html)
package, you can save yourself some time and pain by using one of the many
webapp toolkits available for Go. This article will covers a simple webapp
using
the
[Gorilla web toolkit's](http://www.gorillatoolkit.org/) [`mux`](http://docs.activestate.com/activego/1.8/pkg/github.com/gorilla/mux/index.html) package,
with [Postgres](https://www.postgresql.org/) as the backend database. To talk
to Postgres from Go, we'll
use
[`lib/pq`](http://docs.activestate.com/activego/1.8/pkg/github.com/lib/pq/index.html) with
the
[`database/sql`](http://docs.activestate.com/activego/1.8/pkg/database/sql.1.html) core
package.

All of the packages used in the example application are shipped with
our [ActiveGo beta](http://www.activestate.com/activego/downloads), so you can
download ActiveGo and run this application on your own system.

This article assumes you already have a basic understanding of the Go language
and tools, so I'll focus on the design of the webapp and the use of `mux` and
other libraries.

I'm going to build a simple webapp with a REST API that takes a chunk of text
and gives you the SHA256 hash (in hex form) for that text. It will also go the
other way, from SHA256 to text. This is going to be a **huge** money-maker for
us, so the app does user authorization and billing. To keep this post shorter,
I'll omit the user registration and purchasing parts of the application.

All of the code and tools for this application are in
a
[public GitHub repo](https://github.com/ActiveState/golang-gorilla-webapp.git).

Let's start by taking a look at our database schema<sup>[1](#fn-1)</sup>:

```sql
CREATE TABLE "user" (
    user_id  CHAR(64)   PRIMARY KEY, -- a SHA256 token for web requests
    name     TEXT       NOT NULL,
    credit   BIGINT     DEFAULT 0 -- credits in cents
);

CREATE TABLE hash_text (
    hash     CHAR(64)   PRIMARY KEY,
    text     TEXT
);
```

Now that we have a very sophisticated schema, let's define our REST API. We
need endpoints for turning a text into a hash, one to return the text for a
hash, and for good measure we'll add an endpoint to look up a user by their
`user_id`:

* `GET /user/me` - returns a JSON object containing all of the information we
  have about the user that calls this endpoints.
* `POST /text` - accepts a JSON object with a single key, `text`. We'll hash
  this text and return a JSON object with a single key, `hash`. If the
  requester is out of credit, this returns a 402 (Payment Required).
* `GET /text/{hash}` - returns a JSON object containing the text for a hash.
  If no such hash exists it returns a 404 (Not Found).
  
All endpoints will require that the user supply an `X-HashText-User-ID`
containing a valid `user_id`. Any request without this token will receive a
401 (Unauthorized) response.

So how does this work with Gorilla's `mux`? We'll use `mux` to set up routes
to handle each of these endpoints, as well as to look up the `hash` in the
`/text/{hash}` URI. We set these endpoints in
the
[`router.go`](https://github.com/ActiveState/golang-gorilla-webapp/blob/master/hashtext/router.go) file:

```go
package main

import "github.com/gorilla/mux"

func makeRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/user/me", wrapHandler(userHandler)).Methods("GET")
	r.HandleFunc("/text", wrapHandler(textHandler)).Methods("POST")
	r.HandleFunc("/text/{hash}", wrapHandler(textHashHandler)).Methods("GET")
	return r
}
```

In the code above, we've only matched based on paths and the HTTP methods, but
the `mux.Router` type supports many other options.

So we have some routes, but what are we routing to? The `HandleFunc` method
expects its second argument to be a function with the signature
`(http.ResponseWriter, *http.Request)`. We're taking advantage of Go's excellent
support for first-class functions by calling `wrapHandler()` to wrap all of my
handlers with code to check user authorization. Here's what `wrapHandler()`
looks like in
the
[`handlers.go`](https://github.com/ActiveState/golang-gorilla-webapp/blob/master/hashtext/handlers.go) file:

```go

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
```

This takes any handler function and wraps it with another function. That
wrapper function calls `userIsAuthorized` and returns a 401 (Unauthorized)
response if the user is not authorized. Otherwise it calls the original
handler. All that `userIsAuthorized` does is check that the user ID in the
`X-HashText-User-ID` header exists in the database:

```
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
```

We
call
[`r.Header.Get()`](http://docs.activestate.com/activego/1.8/pkg/http.1.html#Header.Get) to
get the value of the header. If it's not set, or the header exists but is
empty, this value will be an empty string. If that's the case we can skip the
database lookup.

For the database lookup, we
call
[`db.QueryRow`](http://docs.activestate.com/activego/1.8/pkg/database/sql.1.html#DB.QueryRow). This
method is used to execute a query that is expected to return exactly zero or
one row. We then
call
[`Scan()`](http://docs.activestate.com/activego/1.8/pkg/database/sql.1.html#Row.Scan) on
the
returned
[`*db.Row`](http://docs.activestate.com/activego/1.8/pkg/database/sql.1.html#Row) to
get the actual value. If no rows were found, the `Scan()` method will return
a
[`sql.ErrNoRows`](http://docs.activestate.com/activego/1.8/pkg/database/sql.1.html#pkg-variables) error. If
any other sort of error is returned that indicates a more serious problem. If
this were a real production application, we'd want a robust logging system,
but for now we just use
the [`log`](http://docs.activestate.com/activego/1.8/pkg/log/index.html)
package to spit out an error message.

Here's what our `UserHandler` itself looks like:

```go
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
```

Once again we use `db.QueryRow()` to look up a row in the database, this time
to get the user's name credit amount. If we find a matching user, we send a
JSON document as the response. Since all of our handlers can return a JSON
document, we've abstracted that into its own function.

The pattern for sending a response with
the
[`http.ResponseWriter`](http://docs.activestate.com/activego/1.8/pkg/http.1.html#ResponseWriter) interface
always follows the same pattern:

1. Set the response header.
2. Call
   [`WriteHeader(status)`](http://docs.activestate.com/activego/1.8/pkg/http.1.html#ResponseWriter) passing
   the HTTP status for the response.
3. Write the response body
   using
   [`Write()`](http://docs.activestate.com/activego/1.8/pkg/http.1.html#ResponseWriter) if
   there is a response body. Note that the `Write()` method takes an argument
   of the type `[]byte`, not a `string`. You can
   use
   [`io.WriteString()`](http://docs.activestate.com/activego/1.8/pkg/io/index.html#WriteString) if
   you have a string to send.

Here's what `sendJSONResponse` looks like:

```go
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
```

First we try to encode our response as JSON using `json.Marshal()`. If
encoding fails for some reason, we log an error and return a 500.

We then
call
[`w.Header().Set()`](http://docs.activestate.com/activego/1.8/pkg/http.1.html#Header.Set) to
set the `Content-Type` header in the response. Finally, we call `w.Write()` to
write the response body. Since the `json.Marshal()` method function returns
bytes, not a string, we don't need to use `io.WriteString`. If the call to
`Write()` fails, we log an error, but it's too late to change the HTTP status.

The other handlers are mostly similar, but let's take a look at how we handle
a request with a body and request for a path with a variable.  The
`textHandler` looks at the request body:

```go

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
```

We first
call
[`io.ReadAll(r.Body)`](http://docs.activestate.com/activego/1.8/pkg/io/ioutil/index.html#ReadAll) to
get the request body's
content. The
[`http.Request`](http://docs.activestate.com/activego/1.8/pkg/http.1.html#Request) type's
`Body` field implements
the
[`io.ReadCloser`](http://docs.activestate.com/activego/1.8/pkg/io/index.html#ReadCloser) interface,
which means that the `ReadAll` function can read from it directly. We then use
the
[`json.Unmarshal`](http://docs.activestate.com/activego/1.8/pkg/json.html#Unmarshal) function
to convert the request body's JSON content into a struct. If this fails, we
send an error message with a 400 (Bad Request) status. The error message
itself is just plain text.

The `textHashHandler` function looks at the path for a variable:

``` go
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
```

We told `mux` to look for a variable in this path when we created our router:

```go
	r.HandleFunc("/text/{hash}", wrapHandler(textHashHandler)).Methods("GET")
```

This tells `mux` that any path matching `/text/...` matches this handler, and
that the portion after `/text/` is a variable named `hash`. Note that this
only matches one level of path, so a path like `/text/.../more/stuff` would
not match<sup>[2](#fn-2)</sup>. We can look up the matched variables by
calling
[`mux.Vars(r)`](http://docs.activestate.com/activego/1.8/pkg/github.com/gorilla/mux/index.html#Vars). This
returns a `map[string]string` with all the matched variables. We look up the
`hash` key in this map and attempt to look that hash up in the database. If
the hash exists we return the text in a JSON document, otherwise we return a
404 (Not Found).

It looks like we haven't used `mux` for much in this example, but under the
hood it's doing a lot of work for us. If we had to implement the routing
ourselves, along with matching on HTTP methods and looking up variables in the
path, that would require dozens of lines of code. And we've only scratched the
surface of what the `mux` router can do for us!

## Future Improvements

There are many things we could do to improve this code, including:

* Implement proper error logging to syslog.
* Create proper models instead of doing (repetitive) SQL queries in the
  handler code.
* Make the database handle injectable to remove some gross code in our tests.
* Use easyjson for improved JSON performance.
* Return errors in JSON form and do a better job of returning the right error
  codes.
* Adding more endpoints, for example to look up all hashes created by a user,
  to add credit, etc.

----

*All code in this blog post and
[the associated repo](https://github.com/ActiveState/golang-gorilla-webapp.git)
is
[licensed under the MIT license](https://github.com/ActiveState/golang-gorilla-webapp/blob/master/LICENSE).*

<a name="fn-1"></a>1. If you want to follow along at home, you can create the
schema by using the `make-schema` tool in the repo:

    $> cd make-schema
    $> go run ./main.go

This requires that Pg has a user named `hashtext` with the password `hashtext`
that can connect on `127.0.0.1`. This user must have permission to drop and
create databases.

<a name="fn-2"></a>2. This is why we need to use the hex representation of our
SHA256 hash as opposed to Base64, because Base64 can include a forward
slash. Using Base64 would make for some fun-to-debug problems!
