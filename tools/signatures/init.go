package signatures

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"github.com/pocketbase/pocketbase/apis"
)

func GenerateKeyPairs() (pubKey string, privateKey string) {
	bitSize := 4096
	key, _ := rsa.GenerateKey(rand.Reader, bitSize)
	pub := key.Public()
	keyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		},
	)
	pubPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PUBLIC KEY",
			Bytes: x509.MarshalPKCS1PublicKey(pub.(*rsa.PublicKey)),
		},
	)
	return string(pubPEM), string(keyPEM)
}

func SignData(data []byte, privateKey string) (string, error) {
	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return "", apis.NewBadRequestError("failed to parse PEM block containing the key", nil)
	}
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", apis.NewBadRequestError("failed to parse DER encoded private key", err)
	}
	signature, err := rsa.SignPKCS1v15(rand.Reader, priv, 0, data)
	if err != nil {
		return "", apis.NewBadRequestError("failed to sign data", err)
	}
	return base64.RawURLEncoding.EncodeToString(signature), nil
}

func ValidateSignature(payload string, signature string, publicKey string) bool {
	block, _ := pem.Decode([]byte(publicKey))
	if block == nil {
		return false
	}
	pub, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	err = rsa.VerifyPKCS1v15(pub, 0, []byte(payload), sig)
	if err != nil {
		return false
	}
	return true
}
