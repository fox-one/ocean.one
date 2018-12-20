package persistence

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"cloud.google.com/go/spanner"
	number "github.com/MixinNetwork/go-number"
	"github.com/fox-one/ocean.one/config"
	"github.com/fox-one/ocean.one/mixin"
	"google.golang.org/api/iterator"
)

const encryptionHeaderLength = 16

type Broker struct {
	BrokerId         string    `spanner:"broker_id" gorm:"type:varchar(36);PRIMARY_KEY:"`
	BrokerLabel      string    `gorm:"type:varchar(36);"`
	SessionId        string    `spanner:"session_id" gorm:"type:varchar(36);"`
	SessionKey       string    `spanner:"session_key" gorm:"type:varchar(1024);"`
	PINToken         string    `spanner:"pin_token" gorm:"type:varchar(512);"`
	EncryptedPIN     string    `spanner:"encrypted_pin" gorm:"type:varchar(512);"`
	EncryptionHeader []byte    `spanner:"encryption_header" gorm:"type:varchar(1024);"`
	CreatedAt        time.Time `spanner:"created_at"`

	DecryptedPIN string `spanner:"-"`
	Client       *mixin.Client
}

func (p *Spanner) Dapp() (*Broker, error) {
	if p.dapp == nil {
		return nil, fmt.Errorf("dapp not set")
	}
	return p.dapp, nil
}

func (p *Spanner) AllBrokers(ctx context.Context, decryptPIN bool) ([]*Broker, error) {
	it := p.spanner.Single().Query(ctx, spanner.Statement{SQL: "SELECT * FROM brokers"})
	defer it.Stop()

	dapp, err := p.Dapp()
	if err != nil {
		return nil, err
	}

	brokers := []*Broker{
		dapp,
	}

	for {
		row, err := it.Next()
		if err == iterator.Done {
			return brokers, nil
		} else if err != nil {
			return brokers, err
		}
		var broker Broker
		err = row.ToStruct(&broker)
		if err != nil {
			return brokers, err
		}
		if decryptPIN {
			err = broker.DecryptPIN()
			if err != nil {
				return brokers, err
			}

			err = broker.LoadClient()
			if err != nil {
				return brokers, err
			}
		}
		brokers = append(brokers, &broker)
	}
}

func (p *Spanner) AddBroker(ctx context.Context, brokerLabel string) (*Broker, error) {
	dapp, err := p.Dapp()
	if err != nil {
		return nil, err
	}
	broker, err := dapp.AddBroker(ctx, brokerLabel)
	if err != nil {
		return nil, err
	}

	encryptedPIN, encryptionHeader, err := broker.EncryptPIN(ctx, broker.Client.Pin)
	if err != nil {
		return nil, err
	}
	broker.EncryptedPIN = encryptedPIN
	broker.EncryptionHeader = encryptionHeader

	insertBroker, err := spanner.InsertStruct("brokers", broker)
	if err != nil {
		return nil, err
	}
	_, err = p.spanner.Apply(ctx, []*spanner.Mutation{insertBroker})
	return broker, err
}

func (b *Broker) AddBroker(ctx context.Context, brokerLabel string) (*Broker, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return nil, err
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(privateKey.Public())
	if err != nil {
		return nil, err
	}
	sessionSecret := base64.StdEncoding.EncodeToString(publicKeyBytes)

	paras := map[string]string{
		"session_secret": sessionSecret,
	}
	if len(brokerLabel) > 0 {
		paras["full_name"] = fmt.Sprintf("%s %x", brokerLabel, md5.Sum(publicKeyBytes))
	} else {
		paras["full_name"] = fmt.Sprintf("Ocean %x", md5.Sum(publicKeyBytes))
	}
	data, err := json.Marshal(paras)
	if err != nil {
		return nil, err
	}

	body, err := b.Client.SendRequest(ctx, "POST", "/users", data)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data struct {
			UserId    string `json:"user_id"`
			SessionId string `json:"session_id"`
			PinToken  string `json:"pin_token"`
		} `json:"data"`
		Error mixin.Error `json:"error"`
	}
	err = json.Unmarshal(body, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Error.Code > 0 {
		return nil, resp.Error
	}

	broker := &Broker{
		BrokerId:    resp.Data.UserId,
		BrokerLabel: brokerLabel,
		SessionId:   resp.Data.SessionId,
		SessionKey: string(pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
		})),
		PINToken:  resp.Data.PinToken,
		CreatedAt: time.Now(),
	}
	if err := broker.LoadClient(); err != nil {
		return nil, err
	}

	err = broker.setupPIN(ctx)
	if err != nil {
		return nil, err
	}
	return broker, err
}

func (broker *Broker) LoadClient() error {
	block, _ := pem.Decode([]byte(broker.SessionKey))
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return err
	}

	client, err := mixin.CreateMixinClient(broker.BrokerId, broker.SessionId, broker.PINToken, broker.DecryptedPIN, privateKey)
	if err != nil {
		return err
	}

	broker.Client = client
	return nil
}

func (b *Broker) DecryptPIN() error {
	privateBlock, _ := pem.Decode([]byte(config.AssetPrivateKey))
	privateKey, err := x509.ParsePKCS1PrivateKey(privateBlock.Bytes)
	if err != nil {
		return err
	}

	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, b.EncryptionHeader[encryptionHeaderLength:], nil)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return err
	}
	cipherBytes, err := base64.StdEncoding.DecodeString(b.EncryptedPIN)
	if err != nil {
		return err
	}
	iv := cipherBytes[:aes.BlockSize]
	source := cipherBytes[aes.BlockSize:]
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(source, source)

	length := len(source)
	unpadding := int(source[length-1])
	b.DecryptedPIN = string(source[:length-unpadding])
	return nil
}

func (b *Broker) EncryptPIN(ctx context.Context, pin string) (string, []byte, error) {
	aesKey := make([]byte, 32)
	_, err := rand.Read(aesKey)
	if err != nil {
		return "", nil, err
	}
	publicBytes, err := base64.StdEncoding.DecodeString(config.AssetPublicKey)
	if err != nil {
		return "", nil, err
	}
	assetPublicKey, err := x509.ParsePKCS1PublicKey(publicBytes)
	if err != nil {
		return "", nil, err
	}
	aesKeyEncrypted, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, assetPublicKey, aesKey, nil)
	if err != nil {
		return "", nil, err
	}
	encryptionHeader := make([]byte, encryptionHeaderLength)
	encryptionHeader = append(encryptionHeader, aesKeyEncrypted...)

	paddingSize := aes.BlockSize - len(pin)%aes.BlockSize
	paddingBytes := bytes.Repeat([]byte{byte(paddingSize)}, paddingSize)
	plainBytes := append([]byte(pin), paddingBytes...)
	cipherBytes := make([]byte, aes.BlockSize+len(plainBytes))
	iv := cipherBytes[:aes.BlockSize]
	_, err = rand.Read(iv)
	if err != nil {
		return "", nil, err
	}
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", nil, err
	}
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(cipherBytes[aes.BlockSize:], plainBytes)
	return base64.StdEncoding.EncodeToString(cipherBytes), encryptionHeader, nil
}

func (b *Broker) setupPIN(ctx context.Context) error {
	pin, err := generateSixDigitCode(ctx)
	if err != nil {
		return err
	}
	b.Client.Pin = pin
	data, _ := json.Marshal(map[string]string{"pin": b.Client.EncryptPin()})

	body, err := b.Client.SendRequest(ctx, "POST", "/pin/update", data)
	if err != nil {
		return err
	}
	var resp struct {
		Error mixin.Error `json:"error"`
	}
	err = json.Unmarshal(body, &resp)
	if err != nil {
		return err
	}
	if resp.Error.Code > 0 {
		return resp.Error
	}
	return nil
}

func generateSixDigitCode(ctx context.Context) (string, error) {
	var b [8]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return "", err
	}
	c := binary.LittleEndian.Uint64(b[:]) % 1000000
	if c < 100000 {
		c = 100000 + c
	}
	return fmt.Sprint(c), nil
}

type TransferInput struct {
	AssetId     string
	RecipientId string
	Amount      number.Decimal
	TraceId     string
	Memo        string
}

func (broker *Broker) CreateTransfer(ctx context.Context, in *TransferInput) error {
	if in.Amount.Exhausted() {
		return nil
	}

	encryptedPIN := broker.Client.EncryptPin()
	data, err := json.Marshal(map[string]interface{}{
		"asset_id":    in.AssetId,
		"opponent_id": in.RecipientId,
		"amount":      in.Amount.Persist(),
		"trace_id":    in.TraceId,
		"memo":        in.Memo,
		"pin":         encryptedPIN,
	})
	if err != nil {
		return err
	}

	body, err := broker.Client.SendRequest(ctx, "POST", "/transfers", data)
	if err != nil {
		return err
	}

	var resp struct {
		Error mixin.Error `json:"error"`
	}
	err = json.Unmarshal(body, &resp)
	if err != nil {
		return err
	}
	if resp.Error.Code > 0 {
		return resp.Error
	}
	return nil
}
