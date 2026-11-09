package models

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Use a generated test key. Verify the signature and claims at the token HTTP boundary so a
// placeholder bearer token cannot pass the service account tests.
func vertexTestCredentials(t *testing.T) ([]byte, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key)})
	data, err := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "test-project",
		"private_key_id": "test-key-id", "private_key": string(encoded),
		"client_email": "test@test-project.iam.gserviceaccount.com",
		"token_uri":    "https://oauth2.googleapis.com/token"})
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	return data, &key.PublicKey
}

func checkVertexAssertion(t *testing.T, request *http.Request, key *rsa.PublicKey,
	start time.Time,
) {
	t.Helper()
	assert.Equal(t, http.MethodPost, request.Method)
	assert.Equal(t, "", request.Header.Get("Authorization"))
	assert.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
	data, err := io.ReadAll(request.Body)
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	form, err := url.ParseQuery(string(data))
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	assert.Equal(t, url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion": {form.Get("assertion")}}, form)
	parts := strings.Split(form.Get("assertion"), ".")
	if !assert.Len(t, parts, 3) {
		t.FailNow()
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	assert.NoError(t, err)
	assert.JSONEq(t, `{"alg":"RS256","typ":"JWT","kid":"test-key-id"}`, string(header))
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	assert.NoError(t, err)
	var claims map[string]any
	assert.NoError(t, json.Unmarshal(payload, &claims))
	issued, ok := claims["iat"].(float64)
	if !assert.True(t, ok) {
		t.FailNow()
	}
	// Google's signer subtracts a small clock allowance from the current time.
	assert.GreaterOrEqual(t, issued, float64(start.Add(-time.Minute).Unix()))
	assert.LessOrEqual(t, issued, float64(time.Now().Unix()))
	assert.Equal(t, map[string]any{"iss": "test@test-project.iam.gserviceaccount.com",
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   "https://oauth2.googleapis.com/token", "iat": issued, "exp": issued + 3600}, claims)
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	assert.NoError(t, err)
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	assert.NoError(t, rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature))
}

func TestVertexAIServiceAccountAuthentication(t *testing.T) {
	type input struct {
		source  string
		refresh bool
	}
	type expected struct{ exchanges, calls int }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"JSON cache", input{"json", false}, expected{1, 2}},
		{"file cache", input{"file", false}, expected{1, 2}},
		{"ADC file", input{"adc", false}, expected{1, 2}},
		{"JSON refresh", input{"json", true}, expected{2, 2}},
		{"file refresh", input{"file", true}, expected{2, 2}},
		{"ADC refresh", input{"adc", true}, expected{2, 2}},
	}
	data, key := vertexTestCredentials(t)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "")
			start := time.Now()
			actual := expected{}
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.URL.String() == "https://oauth2.googleapis.com/token" {
					actual.exchanges++
					checkVertexAssertion(t, request, key, start)
					expires := 3600
					if test.input.refresh && actual.exchanges == 1 {
						expires = 60
					}
					return fixtureHTTP(fmt.Sprintf(`{"access_token":"test-token-%d",
"token_type":"Bearer","expires_in":%d}`, actual.exchanges, expires), "application/json"), nil
				}
				actual.calls++
				assert.Equal(t, fmt.Sprintf("Bearer test-token-%d", actual.exchanges),
					request.Header.Get("Authorization"))
				assert.Equal(t, "", request.Header.Get("X-Goog-Api-Key"))
				return fixtureHTTP(providerResponse("vertexai", 3), "application/json"), nil
			})}
			config := VertexAIConfig{Project: "test-project", Model: "test-model", HTTPClient: client}
			switch test.input.source {
			case "json":
				config.CredentialsJSON = data
			case "file", "adc":
				path := filepath.Join(t.TempDir(), "test-key.json")
				if !assert.NoError(t, os.WriteFile(path, data, 0600)) {
					return
				}
				if test.input.source == "adc" {
					t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)
				} else {
					config.CredentialsFile = path
				}
			}
			model, err := NewVertexAI(&config)
			if !assert.NoError(t, err) {
				return
			}
			assert.Equal(t, expected{}, actual, "construction must not request a token")
			checkVertexResponse(t, model, vertexTestRequest())
			checkVertexResponse(t, model, vertexTestRequest())
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestVertexAIServiceAccountRefreshCancellation(t *testing.T) {
	data, _ := vertexTestCredentials(t)
	started := make(chan struct{})
	calls := 0
	client := &http.Client{Transport: responseTransport(func(request *http.Request) (
		*http.Response, error,
	) {
		if request.URL.Host != "oauth2.googleapis.com" {
			return fixtureHTTP(providerResponse("vertexai", 3), "application/json"), nil
		}
		calls++
		if calls == 1 {
			return fixtureHTTP(`{"access_token":"test-token","token_type":"Bearer","expires_in":60}`,
				"application/json"), nil
		}
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	model, err := NewVertexAI(&VertexAIConfig{Project: "project", Model: "model",
		CredentialsJSON: data, HTTPClient: client})
	if !assert.NoError(t, err) {
		return
	}
	checkVertexResponse(t, model, vertexTestRequest())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := model.GenerateContentStream(ctx, vertexTestRequest())
		finished <- err
	}()
	select {
	case <-started:
		cancel()
	case <-ctx.Done():
		t.Fatal("refresh did not start")
	}
	assert.ErrorIs(t, <-finished, context.Canceled)
	assert.Equal(t, 2, calls)
}

func TestVertexAIADCUserCredentials(t *testing.T) {
	type input struct{ quotaOverride string }
	type expected struct{ quota string }
	type testCase struct {
		name     string
		input    input
		expected expected
	}
	cases := []testCase{
		{"ADC quota", input{}, expected{"adc-quota-project"}},
		{"explicit quota", input{"explicit-project"}, expected{"explicit-project"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "adc.json")
			data := []byte(`{"type":"authorized_user","client_id":"test-client",
"client_secret":"test-secret","refresh_token":"test-refresh",
"quota_project_id":"adc-quota-project"}`)
			if !assert.NoError(t, os.WriteFile(path, data, 0600)) {
				return
			}
			t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)
			t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "")
			exchanges := 0
			client := &http.Client{Transport: responseTransport(func(request *http.Request) (
				*http.Response, error,
			) {
				if request.URL.String() == "https://oauth2.googleapis.com/token" {
					exchanges++
					assert.NoError(t, request.ParseForm())
					assert.Equal(t, url.Values{"client_id": {"test-client"},
						"client_secret": {"test-secret"}, "refresh_token": {"test-refresh"},
						"grant_type": {"refresh_token"}}, request.PostForm)
					return fixtureHTTP(`{"access_token":"test-user-token","expires_in":3600,
"token_type":"Bearer"}`, "application/json"), nil
				}
				assert.Equal(t, "Bearer test-user-token", request.Header.Get("Authorization"))
				assert.Equal(t, test.expected.quota, request.Header.Get("X-Goog-User-Project"))
				return fixtureHTTP(providerResponse("vertexai", 3), "application/json"), nil
			})}
			model, err := NewVertexAI(&VertexAIConfig{Project: "project", Model: "model",
				HTTPClient: client, QuotaProject: test.input.quotaOverride})
			if !assert.NoError(t, err) {
				return
			}
			checkVertexResponse(t, model, vertexTestRequest())
			checkVertexResponse(t, model, vertexTestRequest())
			assert.Equal(t, 1, exchanges)
		})
	}
}
