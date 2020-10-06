package zip

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func timespecToTime(ts syscall.Timespec) time.Time {

	return time.Unix(int64(ts.Sec), int64(ts.Nsec))
}

func saveArchiveProtectedToStorage(Filename, path, id, password, format string) error {
	// get last modified time
	file, err := os.Stat(Filename)
	if err != nil {
		fmt.Println(err)
	}

	fileFd, err := os.Open(Filename)

	if err != nil {
		return fmt.Errorf("read file error %s", err.Error())
	}
	defer fileFd.Close()
	// Создаем папку для файла

	errorChdir := os.Chdir(path)
	if errorChdir != nil {
		return fmt.Errorf("Chdir error %s", errorChdir.Error())
	}

	errFolder := os.Mkdir(id, os.ModePerm)
	if errFolder != nil {
		return fmt.Errorf("Create direcory error %s", errFolder.Error())
	}

	// Заходим в созданный каталог
	errorChdir = os.Chdir(id)
	if errorChdir != nil {
		return fmt.Errorf("Chdir error %s", errorChdir.Error())
	}

	defer fileFd.Close()

	// читаем в буфер
	buffer := bytes.NewBuffer(make([]byte, 0))
	bufferReader := make([]byte, 1024)
	for {
		n, err := fileFd.Read(bufferReader)
		if err != nil && err != io.EOF {
			return fmt.Errorf("read file error %s", err.Error())
		}
		if n == 0 {
			break
		}

		buffer.Write(bufferReader[:n])
	}

	// Пишем в защищенный архив
	raw := new(bytes.Buffer)
	zipWriter := NewWriter(raw)
	w, err := zipWriter.Encrypt(Filename, password, AES256Encryption, 0x800, file.ModTime())
	if err != nil {
		return fmt.Errorf("encrypt error %s", err.Error())
	}

	_, err = io.Copy(w, bytes.NewReader(buffer.Bytes()))
	if err != nil {
		return fmt.Errorf("cope new reader error %s", err.Error())
	}

	if err := zipWriter.Close(); err != nil {
		return fmt.Errorf("zip close error %s", err.Error())
	}

	// Создаём защищенный архив
	fo, err := os.Create(Filename + format)
	if err != nil {
		return fmt.Errorf("create archive '%s' error %s", Filename+format, err.Error())
	}

	// close fo on exit and check for its returned error
	defer func() {
		if err := fo.Close(); err != nil {
			panic(err)
		}
	}()

	// make a write buffer
	writer := bufio.NewWriter(fo)

	// make a buffer to keep chunks that are read
	buf := make([]byte, 1024)
	r := bytes.NewReader(raw.Bytes())
	for {
		// read a chunk
		n, err := r.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("read buffer error %s", err.Error())
		}
		if n == 0 {
			break
		}

		// write a chunk
		if _, err := writer.Write(buf[:n]); err != nil {
			return fmt.Errorf("writer error %s", err.Error())
		}
	}

	if err = writer.Flush(); err != nil {
		return fmt.Errorf("writer flush error %s", err.Error())
	}

	return nil
}

func TestArchive(t *testing.T) {
	id := strconv.FormatInt(time.Now().Unix(), 9)
	name := "Teams - (имя файла на русском языке!) (1).txt"
	log.Println(">>> ", id)

	err := saveArchiveProtectedToStorage(name, "attach", id, "password", ".zip")
	if err != nil {
		t.Error(err)
	}

}
