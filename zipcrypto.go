package zip

import (
	"bytes"
	"encoding/hex"
	"hash/crc32"
	"io"

	"github.com/pkg/errors"
)

type ZipCrypto struct {
	password         []byte
	Keys             [3]uint32
	encryptionHeader []byte
}

func NewZipCrypto(passphrase []byte) *ZipCrypto {
	z := &ZipCrypto{}
	z.password = passphrase
	z.init()
	return z
}

func (z *ZipCrypto) init() {
	z.Keys[0] = 0x12345678
	z.Keys[1] = 0x23456789
	z.Keys[2] = 0x34567890

	for i := 0; i < len(z.password); i++ {
		z.updateKeys(z.password[i])
	}
}

func (z *ZipCrypto) updateKeys(byteValue byte) {
	z.Keys[0] = crc32update(z.Keys[0], byteValue)
	z.Keys[1] = (z.Keys[1]+z.Keys[0]&0xff)*0x8088405 + 1
	z.Keys[2] = crc32update(z.Keys[2], (byte)(z.Keys[1]>>24))
}

func (z *ZipCrypto) magicByte() byte {
	var t uint32 = z.Keys[2] | 2
	return byte((t * (t ^ 1)) >> 8)
}

func (z *ZipCrypto) Encrypt(data []byte) []byte {
	length := len(data)
	chiper := make([]byte, length)
	for i := 0; i < length; i++ {
		v := data[i]
		chiper[i] = v ^ z.magicByte()
		z.updateKeys(v)
	}
	return chiper
}

func (z *ZipCrypto) Decrypt(chiper []byte) []byte {
	length := len(chiper)
	plain := make([]byte, length)
	for i, c := range chiper {
		v := c ^ z.magicByte()
		z.updateKeys(v)
		plain[i] = v
	}
	return plain
}

func (z *ZipCrypto) CheckPasswordVerification(r *io.SectionReader, f *File) error {
	encryptionHeader := make([]byte, 12)
	_, err := r.Read(encryptionHeader)
	if err != nil {
		return err
	}
	r.Seek(-12, 1)
	z.encryptionHeader = encryptionHeader
	decryptedHeader := z.Decrypt(encryptionHeader)
	z.init()
	if f.Flags&0x8 > 0 {
		if (f.FileHeader.ModifiedTime>>8)&0xff != uint16(decryptedHeader[11]) {
			return errors.Errorf("Invalid Password :: Flags: %d, DecryptedHeader: %s, ModifiedTime: %d", f.Flags, hex.EncodeToString(decryptedHeader), (f.FileHeader.ModifiedTime>>8)&0xff)
		}
	} else if (f.FileHeader.CRC32>>24)&0xff != uint32(decryptedHeader[11]) {
		return errors.Errorf("Invalid Password :: Flags: %d, DecryptedHeader: %s, CRC32: %d", f.Flags, hex.EncodeToString(decryptedHeader), (f.FileHeader.CRC32>>24)&0xff)
	}
	return nil
}

func crc32update(pCrc32 uint32, bval byte) uint32 {
	return crc32.IEEETable[(pCrc32^uint32(bval))&0xff] ^ (pCrc32 >> 8)
}

func ZipCryptoDecryptor(r *io.SectionReader, f *File) (*io.SectionReader, error) {
	z := NewZipCrypto(f.password())
	if err := z.CheckPasswordVerification(r, f); err != nil {
		return nil, err
	}
	b := make([]byte, r.Size())
	r.Read(b)

	m := z.Decrypt(b)
	return io.NewSectionReader(bytes.NewReader(m), 12, int64(len(m))), nil
}

type zipCryptoWriter struct {
	w     io.Writer
	z     *ZipCrypto
	first bool
	fw    *fileWriter
}

func (z *zipCryptoWriter) Write(p []byte) (n int, err error) {
	err = nil
	if z.first {
		z.first = false
		header := []byte{0xF8, 0x53, 0xCF, 0x05, 0x2D, 0xDD, 0xAD, 0xC8, 0x66, 0x3F, 0x8C, 0xAC}
		header = z.z.Encrypt(header)

		crc := z.fw.ModifiedTime
		header[10] = byte(crc)
		header[11] = byte(crc >> 8)

		z.z.init()
		z.w.Write(z.z.Encrypt(header))
		n += 12
	}
	z.w.Write(z.z.Encrypt(p))
	return
}

func ZipCryptoEncryptor(i io.Writer, pass passwordFn, fw *fileWriter) (io.Writer, error) {
	z := NewZipCrypto(pass())
	zc := &zipCryptoWriter{i, z, true, fw}
	return zc, nil
}
