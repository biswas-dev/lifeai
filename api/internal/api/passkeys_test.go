package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"testing"

	gologin "github.com/anchoo2kewl/go-login"
	"github.com/biswas-dev/lifeai/api/internal/auth"
	"github.com/fxamacker/cbor/v2"
)

type testAuthenticator struct {
	key *ecdsa.PrivateKey
	id  []byte
}

func testBase64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func newTestAuthenticator(t *testing.T) *testAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return &testAuthenticator{key: key, id: id}
}

func authData(rp string, flags byte, counter uint32) []byte {
	sum := sha256.Sum256([]byte(rp))
	data := append([]byte{}, sum[:]...)
	data = append(data, flags)
	return binary.BigEndian.AppendUint32(data, counter)
}

func (a *testAuthenticator) registration(t *testing.T, challenge, origin string, flags byte) map[string]any {
	t.Helper()
	data := authData("localhost", flags|0x40, 0)
	data = append(data, make([]byte, 16)...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(a.id)))
	data = append(data, a.id...)
	key, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: a.key.X.FillBytes(make([]byte, 32)), -3: a.key.Y.FillBytes(make([]byte, 32))})
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, key...)
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": data})
	if err != nil {
		t.Fatal(err)
	}
	client, _ := json.Marshal(map[string]string{"type": "webauthn.create", "challenge": challenge, "origin": origin})
	return map[string]any{"id": testBase64(a.id), "rawId": testBase64(a.id), "type": "public-key", "clientExtensionResults": map[string]any{}, "response": map[string]any{"clientDataJSON": testBase64(client), "attestationObject": testBase64(attestation), "transports": []string{"internal"}}}
}

func (a *testAuthenticator) assertion(t *testing.T, challenge, origin, rp string, flags byte, counter uint32, userID int64) map[string]any {
	t.Helper()
	data := authData(rp, flags, counter)
	client, _ := json.Marshal(map[string]string{"type": "webauthn.get", "challenge": challenge, "origin": origin})
	clientHash := sha256.Sum256(client)
	signed := append(append([]byte{}, data...), clientHash[:]...)
	sum := sha256.Sum256(signed)
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": testBase64(a.id), "rawId": testBase64(a.id), "type": "public-key", "clientExtensionResults": map[string]any{}, "response": map[string]any{"clientDataJSON": testBase64(client), "authenticatorData": testBase64(data), "signature": testBase64(signature), "userHandle": testBase64(gologin.UserHandle(userID))}}
}

func optionsFrom(begin map[string]any) map[string]any {
	return begin["options"].(map[string]any)["publicKey"].(map[string]any)
}

func registerTestPasskey(t *testing.T, c *securityClient, a *testAuthenticator) {
	t.Helper()
	begin, _ := c.request("POST", "/api/security/passkeys/begin", nil, 200)
	opts := optionsFrom(begin)
	selection := opts["authenticatorSelection"].(map[string]any)
	if selection["userVerification"] != "required" || selection["residentKey"] != "required" || selection["requireResidentKey"] != true {
		t.Fatal("passkey registration did not require a discoverable verified credential")
	}
	credential := a.registration(t, opts["challenge"].(string), "http://localhost", 0x0d) // UP + UV + backup eligible, not yet backed up.
	out, _ := c.request("POST", "/api/security/passkeys/finish", map[string]any{"challenge": begin["challenge"], "name": "Test authenticator", "credential": credential}, 200)
	c.token = out["token"].(string)
}

func TestPasskeyRegistrationLoginRecoveryAndRevocation(t *testing.T) {
	s, c := securityFixture(t)
	_, _ = enableTestTOTP(t, c)
	a := newTestAuthenticator(t)
	registerTestPasskey(t, c, a)
	var eligible, backed bool
	s.db.QueryRow(`SELECT backup_eligible,backed_up FROM passkeys`).Scan(&eligible, &backed)
	if !eligible || backed {
		t.Fatal("backup eligibility was confused with backup state")
	}
	old := c.token
	begin, _ := c.request("POST", "/api/auth/passkeys/login/begin", nil, 200)
	opts := optionsFrom(begin)
	if opts["userVerification"] != "required" {
		t.Fatal("login did not require device verification")
	}
	credential := a.assertion(t, opts["challenge"].(string), "http://localhost", "localhost", 0x1d, 1, 1) // Now backed up.
	finish := map[string]any{"challenge": begin["challenge"], "credential": credential}
	out, _ := c.request("POST", "/api/auth/passkeys/login/finish", finish, 200)
	claims, err := auth.ValidateToken(out["token"].(string), s.cfg.JWTSecret)
	if err != nil || !claims.MFA || claims.UserID != 1 {
		t.Fatal("verified passkey did not satisfy MFA")
	}
	c.request("POST", "/api/auth/passkeys/login/finish", finish, 401)
	var count uint32
	s.db.QueryRow(`SELECT sign_count,backed_up FROM passkeys`).Scan(&count, &backed)
	if count != 1 || !backed {
		t.Fatal("verified authenticator state not persisted")
	}
	// A fresh, correctly signed challenge with a non-advancing counter is a clone warning.
	begin, _ = c.request("POST", "/api/auth/passkeys/login/begin", nil, 200)
	opts = optionsFrom(begin)
	c.request("POST", "/api/auth/passkeys/login/finish", map[string]any{"challenge": begin["challenge"], "credential": a.assertion(t, opts["challenge"].(string), "http://localhost", "localhost", 0x1d, 1, 1)}, 401)
	deleted, _ := c.request("DELETE", "/api/security/passkeys/"+testBase64(a.id), nil, 200)
	c.token = old
	c.request("GET", "/api/me", nil, 401)
	c.token = deleted["token"].(string)
	begin, _ = c.request("POST", "/api/auth/passkeys/login/begin", nil, 200)
	opts = optionsFrom(begin)
	c.request("POST", "/api/auth/passkeys/login/finish", map[string]any{"challenge": begin["challenge"], "credential": a.assertion(t, opts["challenge"].(string), "http://localhost", "localhost", 0x1d, 2, 1)}, 401)
}

func TestPasskeyRejectsInvalidProofs(t *testing.T) {
	for _, failure := range []string{"origin", "rp", "challenge", "verification", "signature", "user", "browser", "backup-eligibility"} {
		t.Run(failure, func(t *testing.T) {
			_, c := securityFixture(t)
			a := newTestAuthenticator(t)
			registerTestPasskey(t, c, a)
			begin, _ := c.request("POST", "/api/auth/passkeys/login/begin", nil, 200)
			opts := optionsFrom(begin)
			challenge, origin, rp := opts["challenge"].(string), "http://localhost", "localhost"
			flags := byte(0x0d)
			userID := int64(1)
			switch failure {
			case "origin":
				origin = "https://attacker.example"
			case "rp":
				rp = "attacker.example"
			case "challenge":
				challenge = "wrong"
			case "verification":
				flags = 0x09
			case "user":
				userID = 999
			case "backup-eligibility":
				flags = 0x05
			}
			credential := a.assertion(t, challenge, origin, rp, flags, 1, userID)
			if failure == "signature" {
				credential["response"].(map[string]any)["signature"] = testBase64([]byte("not a signature"))
			}
			if failure == "browser" {
				c.cookies = map[string]*http.Cookie{}
			}
			c.request("POST", "/api/auth/passkeys/login/finish", map[string]any{"challenge": begin["challenge"], "credential": credential}, 401)
		})
	}
}

func TestPasskeyRegistrationRejectsUnverifiedOrForeignEnrollment(t *testing.T) {
	for _, failure := range []string{"verification", "origin", "other-account"} {
		t.Run(failure, func(t *testing.T) {
			s, c := securityFixture(t)
			begin, _ := c.request("POST", "/api/security/passkeys/begin", nil, 200)
			opts := optionsFrom(begin)
			a := newTestAuthenticator(t)
			flags := byte(0x0d)
			origin := "http://localhost"
			if failure == "verification" {
				flags = 0x09
			}
			if failure == "origin" {
				origin = "https://attacker.example"
			}
			want := 400
			if failure == "other-account" {
				other, _ := c.request("POST", "/api/auth/signup", map[string]string{"email": "other@example.com", "password": "password123"}, 200)
				c.token = other["token"].(string)
				want = 401
			}
			c.request("POST", "/api/security/passkeys/finish", map[string]any{"challenge": begin["challenge"], "credential": a.registration(t, opts["challenge"].(string), origin, flags)}, want)
			var count int
			s.db.QueryRow(`SELECT count(*) FROM passkeys`).Scan(&count)
			if count != 0 {
				t.Fatal("invalid passkey was persisted")
			}
		})
	}
}
