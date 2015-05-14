/*
Copyright (c) 2014 Ashley Jeffs

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package lib

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

type dummyRegister struct {
	createHandler http.HandlerFunc
	joinHandler   http.HandlerFunc

	errors []error
}

// RegisterPrivate - Register a public endpoint handler with a description.
func (d *dummyRegister) RegisterPublic(endpoint, description string, handler http.HandlerFunc) error {
	err := errors.New("public handler was registered")
	d.errors = append(d.errors, err)
	return err
}

// RegisterPrivate - Register a private endpoint handler with a description.
func (d *dummyRegister) RegisterPrivate(endpoint, description string, handler http.HandlerFunc) error {
	if endpoint == "/test/create" {
		d.createHandler = handler
	} else if endpoint == "/test/join" {
		d.joinHandler = handler
	} else {
		err := fmt.Errorf("unrecognised endpoint: %v", endpoint)
		d.errors = append(d.errors, err)
		return err
	}
	return nil
}

func TestRegister(t *testing.T) {
	dummyRegister := dummyRegister{errors: []error{}}

	config := DefaultTokenAuthenticatorConfig()
	config.AllowCreate = true
	config.HTTPConfig.Path = "/test"

	log, stats := getLoggerAndStats()

	httpAuth := NewHTTPAuthenticator(config, log, stats)

	if err := httpAuth.RegisterHandlers(&dummyRegister); err != nil {
		t.Errorf("Failed to register HTTP auth endpoints: %v", err)
		return
	}

	if len(dummyRegister.errors) > 0 {
		for err := range dummyRegister.errors {
			t.Errorf("%v", err)
		}
		return
	}
}

type dummyWriter struct {
	Token  string `json:"token"`
	header http.Header
}

func (d *dummyWriter) Header() http.Header {
	return d.header
}

func (d *dummyWriter) Write(bytes []byte) (int, error) {
	if err := json.Unmarshal(bytes, d); err != nil {
		return 0, err
	}
	return 0, nil
}

func (d *dummyWriter) WriteHeader(int) {
	// nothing
}

func TestTokens(t *testing.T) {
	dummyRegister := dummyRegister{errors: []error{}}

	config := DefaultTokenAuthenticatorConfig()
	config.AllowCreate = true
	config.HTTPConfig.Path = "/test"
	config.HTTPConfig.ExpiryPeriod = 300

	log, stats := getLoggerAndStats()

	httpAuth := NewHTTPAuthenticator(config, log, stats)

	if err := httpAuth.RegisterHandlers(&dummyRegister); err != nil {
		t.Errorf("Failed to register HTTP auth endpoints: %v", err)
		return
	}

	testKeys := []string{
		"test1",
		"test2",
		"test3",
		"test4",
	}

	testTokens := []string{}

	for _, key := range testKeys {
		bodyReader := bytes.NewReader([]byte(fmt.Sprintf(`{"key_value":"%v"}`, key)))
		req, _ := http.NewRequest("POST", "http://localhost:8001/test/create", bodyReader)

		dWriter := dummyWriter{header: http.Header{}, Token: ""}

		httpAuth.serveGenerateToken(&dWriter, req)
		testTokens = append(testTokens, dWriter.Token)

		stored, ok := httpAuth.tokens[dWriter.Token]
		if !ok {
			t.Errorf("Token not stored for key: %v, %v", dWriter.Token, key)
			t.Errorf("Map: %v", httpAuth.tokens)
		}
		if stored.value != key {
			t.Errorf("key mismatch: %v, %v", stored.value, key)
		}
	}

	for i, key := range testKeys {
		if !httpAuth.AuthoriseJoin(testTokens[i], key) {
			t.Errorf("Failed to authorise: %v, %v", testTokens[i], key)
		}
	}

	for _, token := range testTokens {
		if _, ok := httpAuth.tokens[token]; ok {
			t.Errorf("Key not deleted: %v", token)
		}
	}
}

func TestTokenCleanup(t *testing.T) {
	config := DefaultTokenAuthenticatorConfig()
	config.AllowCreate = true
	config.HTTPConfig.Path = "/test"
	config.HTTPConfig.ExpiryPeriod = 0

	log, stats := getLoggerAndStats()

	httpAuth := NewHTTPAuthenticator(config, log, stats)

	testKeys := []string{
		"test1",
		"test2",
		"test3",
		"test4",
	}

	for _, key := range testKeys {
		bodyReader := bytes.NewReader([]byte(fmt.Sprintf(`{"key_value":"%v"}`, key)))
		req, _ := http.NewRequest("POST", "http://localhost:8001/test/create", bodyReader)

		dWriter := dummyWriter{header: http.Header{}, Token: ""}

		httpAuth.serveGenerateToken(&dWriter, req)
		if _, ok := httpAuth.tokens[dWriter.Token]; ok {
			t.Errorf("Token not cleaned up: %v, %v", dWriter.Token, key)
		}
	}

	if len(httpAuth.tokens) > 0 {
		t.Errorf("Keys not cleaned up: %v", httpAuth.tokens)
	}
}

func TestBadKeys(t *testing.T) {
	config := DefaultTokenAuthenticatorConfig()
	config.AllowCreate = true
	config.HTTPConfig.Path = "/test"
	config.HTTPConfig.ExpiryPeriod = 300

	log, stats := getLoggerAndStats()

	httpAuth := NewHTTPAuthenticator(config, log, stats)

	for i := 0; i < 1000; i++ {
		uuid := GenerateStampedUUID()

		bodyReader := bytes.NewReader([]byte(fmt.Sprintf(`{"key_value":"%v"}`, uuid)))
		req, _ := http.NewRequest("POST", "http://localhost:8001/test/create", bodyReader)

		dWriter := dummyWriter{header: http.Header{}, Token: ""}

		httpAuth.serveGenerateToken(&dWriter, req)
		if _, ok := httpAuth.tokens[dWriter.Token]; !ok {
			t.Errorf("Token not added: %v", dWriter.Token)
		}
	}

	// Check existing tokens with random values
	for token, key := range httpAuth.tokens {
		randomKey := GenerateStampedUUID()

		if httpAuth.AuthoriseJoin(token, randomKey) {
			if key.value != randomKey {
				t.Errorf("Authorised join random key: %v %v", token, randomKey)
			}
		}
		if httpAuth.AuthoriseCreate(token, randomKey) {
			if key.value != randomKey {
				t.Errorf("Authorised create random key: %v %v", token, randomKey)
			}
		}
	}

	// Check random tokens and values
	for i := 0; i < 1000; i++ {
		randomToken := GenerateStampedUUID()
		randomKey := GenerateStampedUUID()

		if httpAuth.AuthoriseJoin(randomToken, randomKey) {
			t.Errorf("Authorised join random key/token: %v %v", randomToken, randomKey)
		}
		if httpAuth.AuthoriseCreate(randomToken, randomKey) {
			t.Errorf("Authorised create random key/token: %v %v", randomToken, randomKey)
		}
	}
}
