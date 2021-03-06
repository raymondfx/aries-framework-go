/*
Copyright SecureKey Technologies Inc. All Rights Reserved.
SPDX-License-Identifier: Apache-2.0
*/

package mysql

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hyperledger/aries-framework-go/pkg/storage"

	"github.com/stretchr/testify/require"

	_ "github.com/go-sql-driver/mysql"
)

const (
	sqlStoreDBURL = "root:my-secret-pw@tcp(127.0.0.1:3306)/"
)

// For these unit tests to run, you must ensure you have a SQL DB instance running at the URL specified in
// sqlStoreDBURL. 'make unit-test' from the terminal will take care of this for you.
// To run the tests manually, start an instance by running the following command in the terminal
// docker run -p 3306:3306 --name MYSQLStoreTest -e MYSQL_ROOT_PASSWORD=my-secret-pw -d mysql:8.0.20

func TestMain(m *testing.M) {
	err := waitForSQLDBToStart()
	if err != nil {
		fmt.Printf(err.Error() +
			". Make sure you start a sqlStoreDB instance using" +
			" 'docker run -p 3306:3306 mysql:8.0.20' before running the unit tests")
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func waitForSQLDBToStart() error {
	db, err := sql.Open("mysql", sqlStoreDBURL)
	if err != nil {
		return err
	}

	timeout := time.After(10 * time.Second)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout: couldn't reach sql db server")
		default:
			err := db.Ping()
			if err != nil {
				return err
			}

			return nil
		}
	}
}

func TestSQLDBStore(t *testing.T) {
	t.Run("Test sql db store put and get", func(t *testing.T) {
		prov, err := NewProvider(sqlStoreDBURL, WithDBPrefix("prefixdb"))
		require.NoError(t, err)
		store, err := prov.OpenStore("test")
		require.NoError(t, err)

		const key = "did:example:124"
		data := []byte("value")

		err = store.Put(key, data)
		require.NoError(t, err)

		doc, err := store.Get(key)
		require.NoError(t, err)
		require.NotEmpty(t, doc)
		require.Equal(t, data, doc)

		// test update
		data = []byte(`{"key1":"value1"}`)
		err = store.Put(key, data)
		require.NoError(t, err)

		doc, err = store.Get(key)
		require.NoError(t, err)
		require.NotEmpty(t, doc)
		require.Equal(t, data, doc)

		did2 := "did:example:789"
		_, err = store.Get(did2)
		require.Error(t, err)
		require.Contains(t, storage.ErrDataNotFound.Error(), err.Error())

		// nil key
		_, err = store.Get("")
		require.Error(t, err)
		require.Equal(t, "key is mandatory", err.Error())

		// nil key
		err = store.Put("", data)
		require.Error(t, err)
		require.Equal(t, "key is mandatory", err.Error())

		err = prov.Close()
		require.NoError(t, err)
	})

	t.Run("Test sql multi store put and get", func(t *testing.T) {
		prov, err := NewProvider(sqlStoreDBURL, WithDBPrefix("prefixdb"))
		require.NoError(t, err)
		const commonKey = "did:example:1"
		data := []byte("value1")

		_, err = prov.OpenStore("")
		require.Error(t, err)
		require.Equal(t, err.Error(), "store name is required")

		// create store 1 & store 2
		store1, err := prov.OpenStore("store1")
		require.NoError(t, err)

		store2, err := prov.OpenStore("store2")
		require.NoError(t, err)

		// put in store 1
		err = store1.Put(commonKey, data)
		require.NoError(t, err)

		// get in store 1 - found
		doc, err := store1.Get(commonKey)
		require.NoError(t, err)
		require.NotEmpty(t, doc)
		require.Equal(t, data, doc)

		// put in store 2
		err = store2.Put(commonKey, data)
		require.NoError(t, err)

		// get in store 2 - found
		doc, err = store2.Get(commonKey)
		require.NoError(t, err)
		require.NotEmpty(t, doc)
		require.Equal(t, data, doc)

		// create new store 3 with same name as store1
		store3, err := prov.OpenStore("store1")
		require.NoError(t, err)

		// get in store 3 - found
		doc, err = store3.Get(commonKey)
		require.NoError(t, err)
		require.NotEmpty(t, doc)
		require.Equal(t, data, doc)

		// store length
		require.Len(t, prov.dbs, 2)
	})
	t.Run("Test put, get, delete, iterator error", func(t *testing.T) {
		prov, err := NewProvider("root:@tcp(127.0.0.1:45454)/")
		require.NoError(t, err)

		storeErr := &sqlDBStore{
			db: prov.db,
		}
		const commonKey = "did:example:1"
		data := []byte("value1")
		// put err
		err = storeErr.Put(commonKey, data)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to insert key and value record")

		// get err
		rows, err := storeErr.Get(commonKey)
		require.Error(t, err)
		require.Nil(t, rows)
		require.Contains(t, err.Error(), "failed to get row")

		err = storeErr.Delete(commonKey)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to delete row")

		itr := storeErr.Iterator(commonKey, "test")
		require.Error(t, itr.Error())
		require.Contains(t, itr.Error().Error(), "failed to query rows")
	})
	t.Run("Test sql db store failures", func(t *testing.T) {
		prov, err := NewProvider("")
		require.Error(t, err)
		require.Contains(t, err.Error(), blankDBPathErrMsg)
		require.Nil(t, prov)

		// Invalid db path
		_, err = NewProvider("root:@tcp(127.0.0.1:45454)")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to open connection")

		prov, err = NewProvider("root:@tcp(127.0.0.1:45454)/")
		require.NoError(t, err)

		store, err := prov.OpenStore("sample")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to create db")
		require.Nil(t, store)
	})

	t.Run("Test the open new connection error", func(t *testing.T) {
		prov, err := NewProvider(sqlStoreDBURL)
		require.NoError(t, err)

		// invalid db url
		prov.dbURL = "fake-url"

		_, err = prov.OpenStore("testErr")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to create new connection fake-url")

		//  valid but not available db url
		prov.dbURL = "root:my-secret-pw@tcp(127.0.0.1:3307)/"

		_, err = prov.OpenStore("testErr")
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to use db testErr")
	})

	t.Run("Test sqlDB multi store close by name", func(t *testing.T) {
		prov, err := NewProvider(sqlStoreDBURL, WithDBPrefix("prefixdb"))
		require.NoError(t, err)

		const commonKey = "did:example:1"
		data := []byte("value1")

		storeNames := []string{"store_1", "store_2", "store_3", "store_4", "store_5"}
		storesToClose := []string{"store_1", "store_3", "store_5"}

		for _, name := range storeNames {
			store, e := prov.OpenStore(name)
			require.NoError(t, e)

			e = store.Put(commonKey, data)
			require.NoError(t, e)
		}

		for _, name := range storeNames {
			store, e := prov.OpenStore(name)
			require.NoError(t, e)

			dataRead, e := store.Get(commonKey)
			require.NoError(t, e)
			require.Equal(t, data, dataRead)
		}

		// verify store length
		require.Len(t, prov.dbs, 5)

		for _, name := range storesToClose {
			store, e := prov.OpenStore(name)
			require.NoError(t, e)
			require.NotNil(t, store)

			e = prov.CloseStore(name)
			require.NoError(t, e)
		}

		// verify store length
		require.Len(t, prov.dbs, 2)

		// try to close non existing db
		err = prov.CloseStore("store_x")
		require.NoError(t, err)

		// verify store length
		require.Len(t, prov.dbs, 2)

		err = prov.Close()
		require.NoError(t, err)

		// verify store length
		require.Empty(t, prov.dbs)

		// try close all again
		err = prov.Close()
		require.NoError(t, err)
	})

	t.Run("Test sql db store iterator", func(t *testing.T) {
		prov, err := NewProvider(sqlStoreDBURL)
		require.NoError(t, err)
		store, err := prov.OpenStore("testIterator")
		require.NoError(t, err)

		const valPrefix = "val-for-%s"
		keys := []string{"abc_123", "abc_124", "abc_125", "abc_126", "jkl_123", "mno_123"}

		for _, key := range keys {
			err = store.Put(key, []byte(fmt.Sprintf(valPrefix, key)))
			require.NoError(t, err)
		}

		itr := store.Iterator("abc_", "abc"+storage.EndKeySuffix)
		verifyItr(t, itr, 4, "abc_")

		itr = store.Iterator("", "")
		verifyItr(t, itr, 0, "")

		itr = store.Iterator("abc_", "mno"+storage.EndKeySuffix)
		verifyItr(t, itr, 6, "")

		itr = store.Iterator("abc_", "mno_123")
		verifyItr(t, itr, 5, "")
	})
}

func TestSQLDBStoreDelete(t *testing.T) {
	const commonKey = "did:example:1234"

	prov, err := NewProvider(sqlStoreDBURL)
	require.NoError(t, err)

	data := []byte("value1")

	// create store 1 & store 2
	store1, err := prov.OpenStore("store1")
	require.NoError(t, err)

	// put in store 1
	err = store1.Put(commonKey, data)
	require.NoError(t, err)

	// get in store 1 - found
	doc, err := store1.Get(commonKey)
	require.NoError(t, err)
	require.NotEmpty(t, doc)
	require.Equal(t, data, doc)

	// now try Delete with an empty key - should fail
	err = store1.Delete("")
	require.EqualError(t, err, "key is mandatory")

	// finally test Delete an existing key
	err = store1.Delete(commonKey)
	require.NoError(t, err)

	doc, err = store1.Get(commonKey)
	require.EqualError(t, err, storage.ErrDataNotFound.Error())
	require.Empty(t, doc)
}

func verifyItr(t *testing.T, itr storage.StoreIterator, count int, prefix string) {
	var vals []string

	for itr.Next() {
		if prefix != "" {
			require.True(t, strings.HasPrefix(string(itr.Key()), prefix))
		}

		vals = append(vals, string(itr.Value()))
	}

	require.Len(t, vals, count)

	itr.Release()
	require.False(t, itr.Next())
	require.Empty(t, itr.Key())
	require.Empty(t, itr.Value())
	require.Error(t, itr.Error())
	require.Contains(t, itr.Error().Error(), "sql: Rows are closed")
}
