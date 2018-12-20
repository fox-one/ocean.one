package mixin

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/satori/go.uuid"
)

type Error struct {
	Status      int    `json:"status"`
	Code        int    `json:"code"`
	Description string `json:"description"`
	trace       string
}

func (sessionError Error) Error() string {
	str, err := json.Marshal(sessionError)
	if err != nil {
		log.Panicln(err)
	}
	return string(str)
}

func (client *Client) loadPinCipher(pinToken string) error {
	token, err := base64.StdEncoding.DecodeString(pinToken)
	if err != nil {
		return err
	}

	keyBytes, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, client.privateKey, token, []byte(client.SessionId))
	if err != nil {
		return err
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return err
	}

	client.PinCipher = block
	return nil
}

func (client *Client) EncryptPin() string {
	pinByte := []byte(client.Pin)
	timeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(timeBytes, uint64(time.Now().Unix()))
	pinByte = append(pinByte, timeBytes...)
	iteratorBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(iteratorBytes, uint64(time.Now().UnixNano()))
	pinByte = append(pinByte, iteratorBytes...)
	padding := aes.BlockSize - len(pinByte)%aes.BlockSize
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	pinByte = append(pinByte, padtext...)
	ciphertext := make([]byte, aes.BlockSize+len(pinByte))
	iv := ciphertext[:aes.BlockSize]
	io.ReadFull(rand.Reader, iv)
	mode := cipher.NewCBCEncrypter(client.PinCipher, iv)
	mode.CryptBlocks(ciphertext[aes.BlockSize:], pinByte)
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func (client *Client) signAuthenticationToken(method, uri, body string) (string, error) {
	expire := time.Now().UTC().Add(time.Hour * 24 * 30 * 3)
	sum := sha256.Sum256([]byte(method + uri + body))

	token := jwt.NewWithClaims(jwt.SigningMethodRS512, jwt.MapClaims{
		"uid": client.ClientId,
		"sid": client.SessionId,
		"iat": time.Now().UTC().Unix(),
		"exp": expire.Unix(),
		"jti": uuid.Must(uuid.NewV4()).String(),
		"sig": hex.EncodeToString(sum[:]),
	})

	return token.SignedString(client.privateKey)
}
