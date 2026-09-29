package gcpkms

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/GokulMV/DocTheRepo/internal/secrets"
)

const key = "projects/p/locations/global/keyRings/r/cryptoKeys/k"

func TestEnvelopeRoundTripThroughCloudKMS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || !strings.HasPrefix(r.URL.Path, "/v1/"+key+":") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if a, _ := base64.StdEncoding.DecodeString(in["additionalAuthenticatedData"]); string(a) != "doctherepo-hub:dek" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, ":encrypt"):
			_ = json.NewEncoder(w).Encode(map[string]string{"ciphertext": in["plaintext"]})
		case strings.HasSuffix(r.URL.Path, ":decrypt"):
			_ = json.NewEncoder(w).Encode(map[string]string{"plaintext": in["ciphertext"]})
		}
	}))
	defer srv.Close()
	e, err := NewWithTokenSource(key, srv.URL, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "tok"}), nil)
	require.NoError(t, err)
	assert.Equal(t, "gcpkms:"+key, e.KeyID())
	box := secrets.NewBox(e)
	ct, err := box.Seal(context.Background(), []byte("sk-key"), []byte("aad"))
	require.NoError(t, err)
	pt, err := box.Open(context.Background(), ct, []byte("aad"))
	require.NoError(t, err)
	assert.Equal(t, "sk-key", string(pt))

	_, err = NewWithTokenSource("alias/x", srv.URL, nil, nil)
	assert.Error(t, err)
	bad, _ := NewWithTokenSource(key, srv.URL, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "wrong"}), nil)
	_, err = bad.WrapKey(context.Background(), []byte("k"))
	assert.Error(t, err)
}
