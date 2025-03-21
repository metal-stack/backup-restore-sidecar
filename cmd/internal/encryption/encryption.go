package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"unicode"

	"github.com/spf13/afero"
)

// suffix is appended on encryption and removed on decryption from given input
const suffix = ".aes"

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

	iv, err := e.readIV(inputReader, block)
	if err != nil {
		return err
	}

	if err := e.decryptFile(inputReader, outputWriter, block, iv); err != nil {
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

// generateIV() returns unique initialization vector of same size as cipher block for encryption
func (e *Encrypter) generateIV(block cipher.Block) ([]byte, error) {
	iv := make([]byte, block.BlockSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	return iv, nil
}

// encryptFile() encrypts infile to outfile using CTR mode (cipher and iv) and appends iv for decryption
func (e *Encrypter) encryptFile(inputReader io.Reader, outputWriter io.Writer, block cipher.Block, iv []byte) error {
	buf := make([]byte, 1024 * 1024 * 10)
	stream := cipher.NewCTR(block, iv)

	_, err := outputWriter.Write(iv)
	if err != nil {
		return fmt.Errorf("could not pretext iv: %w", err)
	}

	for {
		n, err := inputReader.Read(buf)
		if n > 0 {
			stream.XORKeyStream(buf, buf[:n])
			if _, err := outputWriter.Write(buf[:n]); err != nil {
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

// IsEncrypted() tests if target file is encrypted
func IsEncrypted(path string) bool {
	return filepath.Ext(path) == suffix
}

// readIVAndMessageLength() returns initialization vector and message length for decryption
func (e *Encrypter) readIV(inputReader io.Reader, block cipher.Block) ([]byte, error) {
	fmt.Println("read IV")
	iv := make([]byte, block.BlockSize())
	_, err := inputReader.Read(iv)
	if err != nil {
		return nil, err
	}
	return iv, nil
}

// decryptFile() decrypts infile to outfile using CTR mode (cipher and iv)
func (e *Encrypter) decryptFile(inputReader io.Reader, outputWriter io.Writer, block cipher.Block, iv []byte) error {
	buf := make([]byte, 1024)
	stream := cipher.NewCTR(block, iv)

	for {
		n, err := inputReader.Read(buf)
		if n > 0 {
			stream.XORKeyStream(buf, buf[:n])
			if _, err := outputWriter.Write(buf[:n]); err != nil {
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

func (e *Encrypter) Extension() string {
	return suffix
}
