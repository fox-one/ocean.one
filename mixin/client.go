package mixin

import (
	"crypto/cipher"
	"crypto/rsa"
)

type Client struct {
	ClientId  string
	SessionId string
	PinCipher cipher.Block
	Pin       string

	privateKey *rsa.PrivateKey
}

func CreateMixinClient(clientId, sessionId, pinToken, pin string, privateKey *rsa.PrivateKey) (*Client, error) {
	client := &Client{
		ClientId:   clientId,
		SessionId:  sessionId,
		Pin:        pin,
		privateKey: privateKey,
	}

	if err := client.loadPinCipher(pinToken); err != nil {
		return nil, err
	}
	return client, nil
}
