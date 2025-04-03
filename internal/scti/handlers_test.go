// Copyright 2016 Google LLC. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scti

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/transparency-dev/static-ct/internal/testdata"
	"github.com/transparency-dev/static-ct/internal/testonly/storage/posix"
	"github.com/transparency-dev/static-ct/internal/types"
	"github.com/transparency-dev/static-ct/internal/x509util"
	"github.com/transparency-dev/static-ct/storage"
	"github.com/transparency-dev/static-ct/storage/bbolt"
	tessera "github.com/transparency-dev/trillian-tessera"
	posixTessera "github.com/transparency-dev/trillian-tessera/storage/posix"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

var (
	// Test root
	testRootPath = "../testdata/test_root_ca_cert.pem"

	// Arbitrary time for use in tests
	fakeTime       = time.Date(2016, 7, 22, 11, 01, 13, 0, time.UTC)
	fakeTimeMillis = uint64(fakeTime.UnixNano() / nanosPerMilli)

	// Arbitrary origin for tests
	origin = "example.com"
	prefix = "/" + origin

	// Default handler options for tests
	hOpts = HandlerOptions{
		Deadline:           time.Millisecond * 500,
		RequestLog:         &DefaultRequestLog{},
		MaskInternalErrors: false,
		TimeSource:         newFixedTimeSource(fakeTime),
	}
)

type fixedTimeSource struct {
	fakeTime time.Time
}

// newFixedTimeSource creates a fixedTimeSource instance
func newFixedTimeSource(t time.Time) *fixedTimeSource {
	return &fixedTimeSource{fakeTime: t}
}

// Now returns the time value this instance contains
func (f *fixedTimeSource) Now() time.Time {
	return f.fakeTime
}

// setupTestLog creates test TesseraCT log using a POSIX backend.
func setupTestLog(t *testing.T) *log {
	t.Helper()

	signer, err := setupSigner(fakeSignature)
	if err != nil {
		t.Fatalf("Failed to create test signer: %v", err)
	}

	roots := x509util.NewPEMCertPool()
	if err := roots.AppendCertsFromPEMFile(testRootPath); err != nil {
		t.Fatalf("failed to read trusted roots: %v", err)
	}

	cvOpts := ChainValidationOpts{
		trustedRoots:    roots,
		rejectExpired:   false,
		rejectUnexpired: false,
	}

	log, err := NewLog(t.Context(), origin, signer, cvOpts, newPosixStorageFunc(t), newFixedTimeSource(fakeTime))
	if err != nil {
		t.Fatalf("newLog(): %v", err)
	}

	return log
}

// setupTestServer creates a test TesseraCT server with a single endpoint at path.
func setupTestServer(t *testing.T, log *log, path string) *httptest.Server {
	t.Helper()

	handlers := NewPathHandlers(&hOpts, log)
	handler, ok := handlers[path]
	if !ok {
		t.Fatalf("Handler not found: %s", path)
	}

	return httptest.NewServer(handler)
}

// newPosixStorageFunc returns a function to create a new storage.CTStorage instance with:
//   - a POSIX Tessera storage driver
//   - a POSIX issuer storage system
//   - a BBolt deduplication database
func newPosixStorageFunc(t *testing.T) storage.CreateStorage {
	t.Helper()
	return func(ctx context.Context, signer note.Signer) (*storage.CTStorage, error) {
		driver, err := posixTessera.New(ctx, path.Join(t.TempDir(), "log"))
		if err != nil {
			klog.Fatalf("Failed to initialize POSIX Tessera storage driver: %v", err)
		}

		opts := tessera.NewAppendOptions().
			WithCheckpointSigner(signer).
			WithCTLayout()
			// TODO(phboneff): add other options like MaxBatchSize of 1 when implementing
			// additional tests

		appender, _, _, err := tessera.NewAppender(ctx, driver, opts)
		if err != nil {
			klog.Fatalf("Failed to initialize POSIX Tessera appender: %v", err)
		}

		issuerStorage, err := posix.NewIssuerStorage(t.TempDir())
		if err != nil {
			klog.Fatalf("failed to initialize InMemory issuer storage: %v", err)
		}

		beDedupStorage, err := bbolt.NewStorage(path.Join(t.TempDir(), "dedup.db"))
		if err != nil {
			klog.Fatalf("Failed to initialize BBolt deduplication database: %v", err)
		}

		s, err := storage.NewCTStorage(appender, issuerStorage, beDedupStorage)
		if err != nil {
			klog.Fatalf("Failed to initialize CTStorage: %v", err)
		}
		return s, nil
	}
}

func getHandlers(t *testing.T, handlers pathHandlers) pathHandlers {
	t.Helper()
	path := path.Join(prefix, types.GetRootsPath)
	handler, ok := handlers[path]
	if !ok {
		t.Fatalf("%q path not registered", types.GetRootsPath)
	}
	return pathHandlers{path: handler}
}

func postHandlers(t *testing.T, handlers pathHandlers) pathHandlers {
	t.Helper()
	addChainPath := path.Join(prefix, types.AddChainPath)
	addPreChainPath := path.Join(prefix, types.AddPreChainPath)

	addChainHandler, ok := handlers[addChainPath]
	if !ok {
		t.Fatalf("%q path not registered", types.AddPreChainStr)
	}
	addPreChainHandler, ok := handlers[addPreChainPath]
	if !ok {
		t.Fatalf("%q path not registered", types.AddPreChainStr)
	}

	return map[string]appHandler{
		addChainPath:    addChainHandler,
		addPreChainPath: addPreChainHandler,
	}
}

func TestPostHandlersRejectGet(t *testing.T) {
	log := setupTestLog(t)
	handlers := NewPathHandlers(&hOpts, log)

	// Anything in the post handler list should reject GET
	for path, handler := range postHandlers(t, handlers) {
		t.Run(path, func(t *testing.T) {
			s := httptest.NewServer(handler)
			defer s.Close()

			resp, err := http.Get(s.URL + path)
			if err != nil {
				t.Fatalf("http.Get(%s)=(_,%q); want (_,nil)", path, err)
			}
			if got, want := resp.StatusCode, http.StatusMethodNotAllowed; got != want {
				t.Errorf("http.Get(%s)=(%d,nil); want (%d,nil)", path, got, want)
			}
		})
	}
}

func TestGetHandlersRejectPost(t *testing.T) {
	log := setupTestLog(t)
	handlers := NewPathHandlers(&hOpts, log)

	// Anything in the get handler list should reject POST.
	for path, handler := range getHandlers(t, handlers) {
		t.Run(path, func(t *testing.T) {
			s := httptest.NewServer(handler)
			defer s.Close()

			resp, err := http.Post(s.URL+path, "application/json", nil)
			if err != nil {
				t.Fatalf("http.Post(%s)=(_,%q); want (_,nil)", path, err)
			}
			if got, want := resp.StatusCode, http.StatusMethodNotAllowed; got != want {
				t.Errorf("http.Post(%s)=(%d,nil); want (%d,nil)", path, got, want)
			}
		})
	}
}

func TestPostHandlersFailure(t *testing.T) {
	var tests = []struct {
		descr string
		body  io.Reader
		want  int
	}{
		{"nil", nil, http.StatusBadRequest},
		{"''", strings.NewReader(""), http.StatusBadRequest},
		{"malformed-json", strings.NewReader("{ !$%^& not valid json "), http.StatusBadRequest},
		{"empty-chain", strings.NewReader(`{ "chain": [] }`), http.StatusBadRequest},
		{"wrong-chain", strings.NewReader(`{ "chain": [ "test" ] }`), http.StatusBadRequest},
	}

	log := setupTestLog(t)
	handlers := NewPathHandlers(&hOpts, log)

	for path, handler := range postHandlers(t, handlers) {
		t.Run(path, func(t *testing.T) {
			s := httptest.NewServer(handler)

			for _, test := range tests {
				resp, err := http.Post(s.URL+path, "application/json", test.body)
				if err != nil {
					t.Errorf("http.Post(%s,%s)=(_,%q); want (_,nil)", path, test.descr, err)
					continue
				}
				if resp.StatusCode != test.want {
					t.Errorf("http.Post(%s,%s)=(%d,nil); want (%d,nil)", path, test.descr, resp.StatusCode, test.want)
				}
			}
		})
	}
}

func TestNewPathHandlers(t *testing.T) {
	log := setupTestLog(t)
	t.Run("Handlers", func(t *testing.T) {
		handlers := NewPathHandlers(&HandlerOptions{}, log)
		// Check each entrypoint has a handler
		if got, want := len(handlers), len(entrypoints); got != want {
			t.Fatalf("len(info.handler)=%d; want %d", got, want)
		}

		// We want to see the same set of handler names and paths that we think we registered.
		var hNames []entrypointName
		var hPaths []string
		for p, v := range handlers {
			hNames = append(hNames, v.name)
			hPaths = append(hPaths, p)
		}

		if !cmp.Equal(entrypoints, hNames, cmpopts.SortSlices(func(n1, n2 entrypointName) bool {
			return n1 < n2
		})) {
			t.Errorf("Handler names mismatch got: %v, want: %v", hNames, entrypoints)
		}

		entrypaths := []string{prefix + types.AddChainPath, prefix + types.AddPreChainPath, prefix + types.GetRootsPath}
		if !cmp.Equal(entrypaths, hPaths, cmpopts.SortSlices(func(n1, n2 string) bool {
			return n1 < n2
		})) {
			t.Errorf("Handler paths mismatch got: %v, want: %v", hPaths, entrypaths)
		}
	})
}

// TODO(phboneff): use this in followup PR. Leaving here for now to make
// diffs easier to digest in PRs.
// func parseChain(t *testing.T, isPrecert bool, pemChain []string, root *x509.Certificate) (*ctonly.Entry, []*x509.Certificate) {
// 	t.Helper()
// 	pool := loadCertsIntoPoolOrDie(t, pemChain)
// 	leafChain := pool.RawCertificates()
// 	if !leafChain[len(leafChain)-1].Equal(root) {
// 		// The submitted chain may not include a root, but the generated LogLeaf will
// 		fullChain := make([]*x509.Certificate, len(leafChain)+1)
// 		copy(fullChain, leafChain)
// 		fullChain[len(leafChain)] = root
// 		leafChain = fullChain
// 	}
// 	entry, err := entryFromChain(leafChain, isPrecert, fakeTimeMillis)
// 	if err != nil {
// 		t.Fatalf("failed to create entry")
// 	}
// 	return entry, leafChain
// }

func TestGetRoots(t *testing.T) {
	log := setupTestLog(t)
	server := setupTestServer(t, log, path.Join(prefix, "ct/v1/get-roots"))
	defer server.Close()

	t.Run("get-roots", func(t *testing.T) {
		resp, err := http.Get(server.URL + path.Join(prefix, "ct/v1/get-roots"))
		if err != nil {
			t.Fatalf("Failed to get roots: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Unexpected status code: %v", resp.StatusCode)
		}

		var roots types.GetRootsResponse
		err = json.NewDecoder(resp.Body).Decode(&roots)
		if err != nil {
			t.Errorf("Failed to decode response: %v", err)
		}

		if got, want := len(roots.Certificates), 1; got != want {
			t.Errorf("Unexpected number of certificates: got %d, want %d", got, want)
		}

		got, err := base64.StdEncoding.DecodeString(roots.Certificates[0])
		if err != nil {
			t.Errorf("Failed to decode certificate: %v", err)
		}
		want, _ := pem.Decode([]byte(testdata.CACertPEM))
		if !bytes.Equal(got, want.Bytes) {
			t.Errorf("Unexpected root: got %s, want %s", roots.Certificates[0], base64.StdEncoding.EncodeToString(want.Bytes))
		}
	})
}

// TODO(phboneff): this could just be a parseBodyJSONChain test
func TestAddChainWhitespace(t *testing.T) {
	// Throughout we use variants of a hard-coded POST body derived from a chain of:
	// testdata.LeafSignedByFakeIntermediateCertPEM, testdata.FakeIntermediateCertPEM
	cert, rest := pem.Decode([]byte(testdata.CertFromIntermediate))
	if len(rest) > 0 {
		t.Fatalf("got %d bytes remaining after decoding cert, want 0", len(rest))
	}
	certB64 := base64.StdEncoding.EncodeToString(cert.Bytes)
	intermediate, rest := pem.Decode([]byte(testdata.IntermediateFromRoot))
	if len(rest) > 0 {
		t.Fatalf("got %d bytes remaining after decoding intermediate, want 0", len(rest))
	}
	intermediateB64 := base64.StdEncoding.EncodeToString(intermediate.Bytes)

	// Break the JSON into chunks:
	intro := "{\"chain\""
	// followed by colon then the first line of the PEM file
	chunk1a := "[\"" + certB64[:64]
	// straight into rest of first entry
	chunk1b := certB64[64:] + "\""
	// followed by comma then
	chunk2 := "\"" + intermediateB64 + "\""
	epilog := "]}\n"

	var tests = []struct {
		descr string
		body  string
		want  int
	}{
		{
			descr: "valid",
			body:  intro + ":" + chunk1a + chunk1b + "," + chunk2 + epilog,
			want:  http.StatusOK,
		},
		{
			descr: "valid-space-between",
			body:  intro + " : " + chunk1a + chunk1b + " , " + chunk2 + epilog,
			want:  http.StatusOK,
		},
		{
			descr: "valid-newline-between",
			body:  intro + " : " + chunk1a + chunk1b + ",\n" + chunk2 + epilog,
			want:  http.StatusOK,
		},
		{
			descr: "invalid-raw-newline-in-string",
			body:  intro + ":" + chunk1a + "\n" + chunk1b + "," + chunk2 + epilog,
			want:  http.StatusBadRequest,
		},
		{
			descr: "valid-escaped-newline-in-string",
			body:  intro + ":" + chunk1a + "\\n" + chunk1b + "," + chunk2 + epilog,
			want:  http.StatusOK,
		},
	}

	log := setupTestLog(t)
	server := setupTestServer(t, log, path.Join(prefix, "ct/v1/add-chain"))
	defer server.Close()

	for _, test := range tests {
		t.Run(test.descr, func(t *testing.T) {
			resp, err := http.Post(server.URL+"/ct/v1/add-chain", "application/json", strings.NewReader(test.body))
			if err != nil {
				t.Fatalf("http.Post(%s)=(_,%q); want (_,nil)", types.AddChainPath, err)
			}
			if got, want := resp.StatusCode, test.want; got != want {
				t.Errorf("http.Post(%s)=(%d,nil); want (%d,nil)", types.AddChainPath, got, want)
			}
		})
	}
}

func TestAddChain(t *testing.T) {
	var tests = []struct {
		descr string
		chain []string
		want  int
		err   error
	}{
		{
			descr: "leaf-only",
			chain: []string{testdata.CertFromIntermediate},
			want:  http.StatusBadRequest,
		},
		{
			descr: "wrong-entry-type",
			chain: []string{testdata.PreCertFromIntermediate},
			want:  http.StatusBadRequest,
		},
		{
			descr: "success-without-root",
			chain: []string{testdata.CertFromIntermediate, testdata.IntermediateFromRoot},
			want:  http.StatusOK,
		},
		{
			descr: "success",
			chain: []string{testdata.CertFromIntermediate, testdata.IntermediateFromRoot, testdata.CACertPEM},
			want:  http.StatusOK,
		},
	}

	log := setupTestLog(t)
	server := setupTestServer(t, log, path.Join(prefix, "ct/v1/add-chain"))
	defer server.Close()

	for _, test := range tests {
		t.Run(test.descr, func(t *testing.T) {
			pool := loadCertsIntoPoolOrDie(t, test.chain)
			chain := createJSONChain(t, *pool)

			resp, err := http.Post(server.URL+"/ct/v1/add-chain", "application/json", chain)
			if err != nil {
				t.Fatalf("http.Post(%s)=(_,%q); want (_,nil)", types.AddChainPath, err)
			}
			if got, want := resp.StatusCode, test.want; got != want {
				t.Errorf("http.Post(%s)=(%d,nil); want (%d,nil)", types.AddChainPath, got, want)
			}
			if test.want == http.StatusOK {
				var gotRsp types.AddChainResponse
				if err := json.NewDecoder(resp.Body).Decode(&gotRsp); err != nil {
					t.Fatalf("json.Decode()=%v; want nil", err)
				}
				if got, want := types.Version(gotRsp.SCTVersion), types.V1; got != want {
					t.Errorf("resp.SCTVersion=%v; want %v", got, want)
				}
				if got, want := gotRsp.ID, demoLogID[:]; !bytes.Equal(got, want) {
					t.Errorf("resp.ID=%v; want %v", got, want)
				}
				if got, want := gotRsp.Timestamp, fakeTimeMillis; got != want {
					t.Errorf("resp.Timestamp=%d; want %d", got, want)
				}
				if got, want := hex.EncodeToString(gotRsp.Signature), "040300067369676e6564"; got != want {
					t.Errorf("resp.Signature=%s; want %s", got, want)
				}
				// TODO(phboneff): read from the log and compare values
				// TODO(phboneff): add a test with a backend write failure
				// TODO(phboneff): check that the index is in the SCT
				// TODO(phboneff): add duplicate tests
			}
		})
	}
}

func TestAddPreChain(t *testing.T) {
	var tests = []struct {
		descr string
		chain []string
		want  int
		err   error
	}{
		{
			descr: "leaf-signed-by-different",
			chain: []string{testdata.PrecertPEMValid, testdata.FakeIntermediateCertPEM},
			want:  http.StatusBadRequest,
		},
		{
			descr: "wrong-entry-type",
			chain: []string{testdata.TestCertPEM},
			want:  http.StatusBadRequest,
		},
		{
			descr: "success",
			chain: []string{testdata.PrecertPEMValid, testdata.CACertPEM},
			want:  http.StatusOK,
		},
		{
			descr: "success-with-intermediate",
			chain: []string{testdata.PreCertFromIntermediate, testdata.IntermediateFromRoot, testdata.CACertPEM},
			want:  http.StatusOK,
		},
		{
			descr: "success-without-root",
			chain: []string{testdata.PrecertPEMValid},
			want:  http.StatusOK,
		},
	}

	log := setupTestLog(t)
	server := setupTestServer(t, log, path.Join(prefix, "ct/v1/add-pre-chain"))
	defer server.Close()

	for _, test := range tests {
		t.Run(test.descr, func(t *testing.T) {
			pool := loadCertsIntoPoolOrDie(t, test.chain)
			chain := createJSONChain(t, *pool)

			resp, err := http.Post(server.URL+"/ct/v1/add-pre-chain", "application/json", chain)
			if err != nil {
				t.Fatalf("http.Post(%s)=(_,%q); want (_,nil)", types.AddPreChainPath, err)
			}
			if got, want := resp.StatusCode, test.want; got != want {
				t.Errorf("http.Post(%s)=(%d,nil); want (%d,nil)", types.AddPreChainPath, got, want)
			}
			if test.want == http.StatusOK {
				var gotRsp types.AddChainResponse
				if err := json.NewDecoder(resp.Body).Decode(&gotRsp); err != nil {
					t.Fatalf("json.Decode()=%v; want nil", err)
				}
				if got, want := types.Version(gotRsp.SCTVersion), types.V1; got != want {
					t.Errorf("resp.SCTVersion=%v; want %v", got, want)
				}
				if got, want := gotRsp.ID, demoLogID[:]; !bytes.Equal(got, want) {
					t.Errorf("resp.ID=%v; want %v", got, want)
				}
				if got, want := gotRsp.Timestamp, fakeTimeMillis; got != want {
					t.Errorf("resp.Timestamp=%d; want %d", got, want)
				}
				if got, want := hex.EncodeToString(gotRsp.Signature), "040300067369676e6564"; got != want {
					t.Errorf("resp.Signature=%s; want %s", got, want)
				}
				// TODO(phboneff): read from the log and compare values
				// TODO(phboneff): add a test with a backend write failure
				// TODO(phboneff): check that the index is in the SCT
				// TODO(phboneff): add duplicate tests
			}
		})
	}
}

func createJSONChain(t *testing.T, p x509util.PEMCertPool) io.Reader {
	t.Helper()
	var req types.AddChainRequest
	for _, rawCert := range p.RawCertificates() {
		req.Chain = append(req.Chain, rawCert.Raw)
	}

	var buffer bytes.Buffer
	// It's tempting to avoid creating and flushing the intermediate writer but it doesn't work
	writer := bufio.NewWriter(&buffer)
	err := json.NewEncoder(writer).Encode(&req)
	if err := writer.Flush(); err != nil {
		t.Error(err)
	}

	if err != nil {
		t.Fatalf("Failed to create test json: %v", err)
	}

	return bufio.NewReader(&buffer)
}

func loadCertsIntoPoolOrDie(t *testing.T, certs []string) *x509util.PEMCertPool {
	t.Helper()
	pool := x509util.NewPEMCertPool()
	for _, cert := range certs {
		if !pool.AppendCertsFromPEM([]byte(cert)) {
			t.Fatalf("couldn't parse test certs: %v", certs)
		}
	}
	return pool
}
