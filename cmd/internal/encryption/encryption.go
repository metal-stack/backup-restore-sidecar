package encryption

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/spf13/afero"
)

// suffix is appended on encryption and removed on decryption from given input
const Suffix = ".aes"

// Encrypter is used to encrypt/decrypt backups
type Encrypter struct {
	fs  afero.Fs
	key string
	log *slog.Logger
}

type EncrypterConfig struct {
	FS  afero.Fs
	Key string
}

// New creates a new Encrypter with the given key.
// The key should be 32 bytes (AES-256)
func New(log *slog.Logger, config *EncrypterConfig) (*Encrypter, error) {
	if len(config.Key) != 32 {
		return nil, fmt.Errorf("key length: %d invalid, must be 32 bytes", len(config.Key))
	}
	if !isASCII(config.Key) {
		return nil, fmt.Errorf("key must only contain ascii characters")
	}
	if config.FS == nil {
		config.FS = afero.NewOsFs()
	}

	return &Encrypter{
		log: log,
		key: config.Key,
		fs:  config.FS,
	}, nil

}

// Encrypt input file with key and store encrypted result with suffix
func (e *Encrypter) Encrypt(inputReader io.Reader, outputWriter io.Writer) error {
	block, err := e.createCipher()
	if err != nil {
		return err
	}

	iv, err := e.generateIV(block)
	if err != nil {
		return err
	}

	if err := e.encryptFile(inputReader, outputWriter, block, iv); err != nil {
		return err
	}

	return nil
}

// Decrypt input file with key and store decrypted result with suffix removed
// if input does not end with suffix, it is assumed that the file was not encrypted.
func (e *Encrypter) Decrypt(inputReader io.Reader, outputWriter io.Writer) error {

	block, err := e.createCipher()
	if err != nil {
		return err
	}

	// Erstelle einen Puffer, der den gesamten Input speichert
	var buf bytes.Buffer

	// Kopiere die Daten aus inputReader in den Puffer
	_, err = io.Copy(&buf, inputReader)
	if err != nil {
		return err
	}

	iv, msgLen, err := e.readIVAndMessageLength(buf, block)
	if err != nil {
		return err
	}

	if err := e.decryptFile(bytes.NewReader(buf.Bytes()), outputWriter, block, iv, msgLen); err != nil {
		return err
	}
	return nil
}

func isASCII(s string) bool {
	for _, c := range s {
		if c > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// createCipher() returns new cipher block for encryption/decryption based on encryption-key
func (e *Encrypter) createCipher() (cipher.Block, error) {
	key := []byte(e.key)
	return aes.NewCipher(key)
}

// generateIV() returns unique initalization vector of same size as cipher block for encryption
func (e *Encrypter) generateIV(block cipher.Block) ([]byte, error) {
	iv := make([]byte, block.BlockSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	return iv, nil
}

// encryptFile() encrypts infile to outfile using CTR mode (cipher and iv) and appends iv for decryption
func (e *Encrypter) encryptFile(infile io.Reader, outfile io.Writer, block cipher.Block, iv []byte) error {
	buf := make([]byte, 1024)
	stream := cipher.NewCTR(block, iv)
	for {
		n, err := infile.Read(buf)
		if n > 0 {
			stream.XORKeyStream(buf, buf[:n])
			if _, err := outfile.Write(buf[:n]); err != nil {
				return err
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading from file (%d bytes read): %w", n, err)
		}
	}

	// Append the IV to the outfile
	if _, err := outfile.Write(iv); err != nil {
		return fmt.Errorf("could not append iv: %w", err)
	}

	return nil
}

// IsEncrypted() tests if target file is encrypted
func IsEncrypted(path string) bool {
	return filepath.Ext(path) == Suffix
}

// TrimSuffix() removes encryption suffix from path
func TrimSuffix(path string) string {
	return strings.TrimSuffix(path, Suffix)
}

// readIVAndMessageLength() returns initialization vector and message length for decryption
func (e *Encrypter) readIVAndMessageLength(buf bytes.Buffer, block cipher.Block) ([]byte, int64, error) {
	blockSize := block.BlockSize()

	data := buf.Bytes()
	if len(data) < blockSize {
		return nil, 0, errors.New("data too short to contain iv")
	}

	iv := data[len(data)-blockSize:]
	msgLen := int64(len(data) - blockSize)

	return iv, msgLen, nil
}

// decryptFile() decrypts infile to outfile using CTR mode (cipher and iv)
func (e *Encrypter) decryptFile(infile io.Reader, outfile io.Writer, block cipher.Block, iv []byte, msgLen int64) error {
	buf := make([]byte, 1024)
	stream := cipher.NewCTR(block, iv)

	for {
		n, err := infile.Read(buf)
		if n > 0 {
			if n > int(msgLen) {
				n = int(msgLen)
			}
			msgLen -= int64(n)
			stream.XORKeyStream(buf, buf[:n])
			if _, err := outfile.Write(buf[:n]); err != nil {
				return err
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading from file (%d bytes read): %w", n, err)
		}
	}

	return nil
}
