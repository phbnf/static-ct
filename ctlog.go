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

package sctfe

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	ct "github.com/google/certificate-transparency-go"
	"github.com/google/certificate-transparency-go/asn1"
	"github.com/google/certificate-transparency-go/x509"
	"github.com/google/certificate-transparency-go/x509util"
	"github.com/rs/cors"
	"github.com/transparency-dev/static-ct/internal/scti"
	"github.com/transparency-dev/static-ct/storage"
	"golang.org/x/mod/sumdb/note"
	"k8s.io/klog/v2"
)

// ChainValidationConfig contains parameters to configure chain validation.
type ChainValidationConfig struct {
	// RootsPEMFile is the path to the file containing root certificates that
	// are acceptable to the log. The certs are served through get-roots
	// endpoint.
	RootsPEMFile string
	// RejectExpired controls if true then the certificate validity period will be
	// checked against the current time during the validation of submissions.
	// This will cause expired certificates to be rejected.
	RejectExpired bool
	// RejectUnexpired controls if the SCTFE rejects certificates that are
	// either currently valid or not yet valid.
	// TODO(phboneff): evaluate whether we need to keep this one.
	RejectUnexpired bool
	// ExtKeyUsages lists Extended Key Usage values that newly submitted
	// certificates MUST contain. By default all are accepted. The
	// values specified must be ones known to the x509 package, comma separated.
	ExtKeyUsages string
	// RejectExtensions lists X.509 extension OIDs that newly submitted
	// certificates MUST NOT contain. Empty by default. Values must be
	// specificed in dotted string form (e.g. "2.3.4.5").
	RejectExtensions string
	// NotAfterStart defines the start of the range of acceptable NotAfter
	// values, inclusive.
	// Leaving this unset implies no lower bound to the range.
	NotAfterStart *time.Time
	// NotAfterLimit defines the end of the range of acceptable NotAfter values,
	// exclusive.
	// Leaving this unset implies no upper bound to the range.
	NotAfterLimit *time.Time
}

// CreateStorage instantiates a Tessera storage implementation with a signer option.
type CreateStorage func(context.Context, note.Signer) (*storage.CTStorage, error)

// systemTimeSource implments scti.TimeSource.
type systemTimeSource struct{}

// Now returns the true current local time.
func (s systemTimeSource) Now() time.Time {
	return time.Now()
}

var timeSource = systemTimeSource{}

func newLog(ctx context.Context, origin string, signer crypto.Signer, cfg ChainValidationConfig, cs CreateStorage) (*scti.Log, error) {
	log := &scti.Log{}

	if origin == "" {
		return nil, errors.New("empty origin")
	}
	log.Origin = origin

	// Validate signer that only ECDSA is supported.
	if signer == nil {
		return nil, errors.New("empty signer")
	}
	switch keyType := signer.Public().(type) {
	case *ecdsa.PublicKey:
	default:
		return nil, fmt.Errorf("unsupported key type: %v", keyType)
	}

	log.SignSCT = func(leaf *ct.MerkleTreeLeaf) (*ct.SignedCertificateTimestamp, error) {
		return scti.BuildV1SCT(signer, leaf)
	}

	vlc, err := newCertValidationOpts(cfg)
	if err != nil {
		return nil, fmt.Errorf("invalid cert validation config: %v", err)
	}
	log.ChainValidationOpts = *vlc

	cpSigner, err := scti.NewCpSigner(signer, origin, timeSource)
	if err != nil {
		klog.Exitf("failed to create checkpoint Signer: %v", err)
	}

	storage, err := cs(ctx, cpSigner)
	if err != nil {
		klog.Exitf("failed to initiate storage backend: %v", err)
	}
	log.Storage = storage

	return log, nil
}

// newCertValidationOpts checks that a chain validation config is valid,
// parses it, and loads resources to validate chains.
func newCertValidationOpts(cfg ChainValidationConfig) (*scti.ChainValidationOpts, error) {
	// Load the trusted roots.
	if cfg.RootsPEMFile == "" {
		return nil, errors.New("empty rootsPemFile")
	}
	roots := x509util.NewPEMCertPool()
	if err := roots.AppendCertsFromPEMFile(cfg.RootsPEMFile); err != nil {
		return nil, fmt.Errorf("failed to read trusted roots: %v", err)
	}

	if cfg.RejectExpired && cfg.RejectUnexpired {
		return nil, errors.New("configuration would reject all certificates")
	}

	// Validate the time interval.
	if cfg.NotAfterStart != nil && cfg.NotAfterLimit != nil && (cfg.NotAfterLimit).Before(*cfg.NotAfterStart) {
		return nil, fmt.Errorf("'Not After' limit %q before start %q", cfg.NotAfterLimit.Format(time.RFC3339), cfg.NotAfterStart.Format(time.RFC3339))
	}

	validationOpts := scti.ChainValidationOpts{
		TrustedRoots:    roots,
		RejectExpired:   cfg.RejectExpired,
		RejectUnexpired: cfg.RejectUnexpired,
		NotAfterStart:   cfg.NotAfterStart,
		NotAfterLimit:   cfg.NotAfterLimit,
	}

	// Filter which extended key usages are allowed.
	lExtKeyUsages := []string{}
	if cfg.ExtKeyUsages != "" {
		lExtKeyUsages = strings.Split(cfg.ExtKeyUsages, ",")
	}
	// Validate the extended key usages list.
	for _, kuStr := range lExtKeyUsages {
		if ku, ok := stringToKeyUsage[kuStr]; ok {
			// If "Any" is specified, then we can ignore the entire list and
			// just disable EKU checking.
			if ku == x509.ExtKeyUsageAny {
				klog.Info("Found ExtKeyUsageAny, allowing all EKUs")
				validationOpts.ExtKeyUsages = nil
				break
			}
			validationOpts.ExtKeyUsages = append(validationOpts.ExtKeyUsages, ku)
		} else {
			return nil, fmt.Errorf("unknown extended key usage: %s", kuStr)
		}
	}
	// Filter which extensions are rejected.
	var err error
	if cfg.RejectExtensions != "" {
		lRejectExtensions := strings.Split(cfg.RejectExtensions, ",")
		validationOpts.RejectExtIds, err = parseOIDs(lRejectExtensions)
		if err != nil {
			return nil, fmt.Errorf("failed to parse RejectExtensions: %v", err)
		}
	}

	return &validationOpts, nil
}

func parseOIDs(oids []string) ([]asn1.ObjectIdentifier, error) {
	ret := make([]asn1.ObjectIdentifier, 0, len(oids))
	for _, s := range oids {
		bits := strings.Split(s, ".")
		var oid asn1.ObjectIdentifier
		for _, n := range bits {
			p, err := strconv.Atoi(n)
			if err != nil {
				return nil, err
			}
			oid = append(oid, p)
		}
		ret = append(ret, oid)
	}
	return ret, nil
}

var stringToKeyUsage = map[string]x509.ExtKeyUsage{
	"Any":                        x509.ExtKeyUsageAny,
	"ServerAuth":                 x509.ExtKeyUsageServerAuth,
	"ClientAuth":                 x509.ExtKeyUsageClientAuth,
	"CodeSigning":                x509.ExtKeyUsageCodeSigning,
	"EmailProtection":            x509.ExtKeyUsageEmailProtection,
	"IPSECEndSystem":             x509.ExtKeyUsageIPSECEndSystem,
	"IPSECTunnel":                x509.ExtKeyUsageIPSECTunnel,
	"IPSECUser":                  x509.ExtKeyUsageIPSECUser,
	"TimeStamping":               x509.ExtKeyUsageTimeStamping,
	"OCSPSigning":                x509.ExtKeyUsageOCSPSigning,
	"MicrosoftServerGatedCrypto": x509.ExtKeyUsageMicrosoftServerGatedCrypto,
	"NetscapeServerGatedCrypto":  x509.ExtKeyUsageNetscapeServerGatedCrypto,
}

func newPathHandlers(deadline time.Duration, maskInternalErrors bool, log *scti.Log) scti.PathHandlers {
	opts := &scti.HandlerOptions{
		Deadline:           deadline,
		RequestLog:         &scti.DefaultRequestLog{},
		MaskInternalErrors: maskInternalErrors,
		TimeSource:         timeSource,
	}

	return scti.NewPathHandlers(opts, log)
}

func NewCTHTTPServer(ctx context.Context, origin string, signer crypto.Signer, cfg ChainValidationConfig, cs CreateStorage, httpDeadline time.Duration, maskInternalErrors bool) (*http.ServeMux, error) {
	log, err := newLog(ctx, origin, signer, cfg, cs)
	if err != nil {
		klog.Exitf("Invalid log config: %v", err)
	}

	handlers := newPathHandlers(httpDeadline, maskInternalErrors, log)

	// Allow cross-origin requests to all handlers registered on corsMux.
	// This is safe for CT log handlers because the log is public and
	// unauthenticated so cross-site scripting attacks are not a concern.
	corsMux := http.NewServeMux()
	corsHandler := cors.AllowAll().Handler(corsMux)
	http.Handle("/", corsHandler)

	// Register handlers for all the configured logs.
	for path, handler := range handlers {
		corsMux.Handle(path, handler)
	}

	return corsMux, nil
}
