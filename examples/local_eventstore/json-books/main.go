package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/textileio/go-threads/core/thread"
	"github.com/textileio/go-threads/db"
	"github.com/textileio/go-threads/util"
	"github.com/tidwall/sjson"
)

var (
	jsonSchema = `{
		"$schema": "http://json-schema.org/draft-04/schema#",
		"$ref": "#/definitions/book",
		"definitions": {
		   "book": {
			  "required": [
				 "ID",
				 "Title",
				 "Author",
				 "Meta"
			  ],
			  "properties": {
				 "Author": {
					"type": "string"
				 },
				 "ID": {
					"type": "string"
				 },
				 "Meta": {
					"$schema": "http://json-schema.org/draft-04/schema#",
					"$ref": "#/definitions/bookStats"
				 },
				 "Title": {
					"type": "string"
				 }
			  },
			  "additionalProperties": false,
			  "type": "object"
		   },
		   "bookStats": {
			  "required": [
				 "TotalReads",
				 "Rating"
			  ],
			  "properties": {
				 "Rating": {
					"type": "number"
				 },
				 "TotalReads": {
					"type": "integer"
				 }
			  },
			  "additionalProperties": false,
			  "type": "object"
		   }
		}
	 }`
)

func main() {
	d, clean := createMemDB()
	defer clean()

	collection, err := d.NewCollection(db.CollectionConfig{Name: "Book", Schema: util.SchemaFromSchemaString(jsonSchema)})
	checkErr(err)

	// Bootstrap the collection with some books: two from Author1 and one from Author2
	{
		// Create a two books for Author1
		book1 := []byte(`{"ID": "", "Title": "Title1", "Author": "Author1", "Meta": { "TotalReads": 100, "Rating": 3.2 }}`) // Notice ID will be autogenerated
		book2 := []byte(`{"ID": "", "Title": "Title2", "Author": "Author1", "Meta": { "TotalReads": 150, "Rating": 4.1 }}`)
		ids, err := collection.Create(book1, book2) // Note you can create multiple books at the same time (variadic)
		checkErr(err)
		// You can see here the ids of the newly saved instances. You'll want to update the ID property of
		fmt.Printf("Instance ids of stored books: %v\n", ids)

		// Raw json editing :)
		book1, err = sjson.SetBytes(book1, "ID", ids[0].String())
		checkErr(err)
		book2, err = sjson.SetBytes(book2, "ID", ids[1].String())
		checkErr(err)

		editedBook2, err := sjson.SetBytes(book2, "Meta.TotalReads", 100000)
		checkErr(collection.Save(editedBook2))

		// Try .Has(...) for book2
		instanceID := ids[1]
		exists, err := collection.Has(instanceID)
		checkErr(err)
		if !exists {
			panic("instance should exist")
		}

		// Try getting it by ID
		foundEditedBook2, err := collection.FindByID(instanceID)
		checkErr(err)
		fmt.Printf("Book2 after edition: %s\n", string(foundEditedBook2))
		if !strings.Contains(string(foundEditedBook2), "100000") {
			panic("book2 doesn't have updated information")
		}

		checkErr(collection.Delete(instanceID))

		exists, err = collection.Has(instanceID)
		checkErr(err)
		if exists {
			panic("instance shouldn't exist")
		}
	}
}

func createMemDB() (*db.DB, func()) {
	dir, err := ioutil.TempDir("", "")
	checkErr(err)
	n, err := db.DefaultNetwork(dir)
	checkErr(err)
	id := thread.NewIDV1(thread.Raw, 32)
	d, err := db.NewDB(context.Background(), n, thread.NewDefaultCreds(id), db.WithRepoPath(dir))
	checkErr(err)
	return d, func() {
		time.Sleep(time.Second) // Give threads a chance to finish work
		if err := n.Close(); err != nil {
			panic(err)
		}
		_ = os.RemoveAll(dir)
	}
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}
