This fork add support for Standard Zip Encryption.

The work is based on https://github.com/alexmullins/zip

Available encryption:

```
zip.StandardEncryption
zip.AES128Encryption
zip.AES192Encryption
zip.AES256Encryption
```

## Warning

Zip Standard Encryption isn't actually secure(https://github.com/alexmullins/zip/issues/17).
Unless you have to work with it, please use AES encryption instead.

## Example Encrypt Zip

```
package main

import (
	"bytes"
	"io"
	"log"
	"os"

	"github.com/yeka/zip"
)

func main() {
	contents := []byte("Hello World")
	fzip, err := os.Create(`./test.zip`)
	if err != nil {
		log.Fatalln(err)
	}
	zipw := zip.NewWriter(fzip)
	defer zipw.Close()
	w, err := zipw.Encrypt(`test.txt`, `golang`, zip.AES256Encryption)
	if err != nil {
		log.Fatal(err)
	}
	_, err = io.Copy(w, bytes.NewReader(contents))
	if err != nil {
		log.Fatal(err)
	}
	zipw.Flush()
}
```

## Example Decrypt Zip

```
package main

import (
	"fmt"
	"io/ioutil"
	"log"

	"github.com/yeka/zip"
)

func main() {
	r, err := zip.OpenReader("encrypted.zip")
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.IsEncrypted() {
			f.SetPassword("12345")
		}

		r, err := f.Open()
		if err != nil {
			log.Fatal(err)
		}

		buf, err := ioutil.ReadAll(r)
		if err != nil {
			log.Fatal(err)
		}
		defer r.Close()

		fmt.Printf("Size of %v: %v byte(s)\n", f.Name, len(buf))
	}
}
```


## Example Encrypt Multiple Files

```
// ZipFilesWithEncrypt encrypts several files at once. Absolve paths are filenames and files. 
func ZipFilesWithEncrypt(fileName string, files []string, password string) error {
	zipFile, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipw := zip.NewWriter(zipFile)
	for _, f := range files {
		if err := appendFiles(f, password, zipw); err != nil {
			return err
		}
	}
	if err = zipw.Close(); err != nil {
		return err
	}
	return nil
}

func appendFiles(filename, password string, zipw *zip.Writer) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	name := path.Base(filename)

	wr, err := zipw.Encrypt(name, password, zip.StandardEncryption)
	if err != nil {
		return err
	}

	if _, err := io.Copy(wr, file); err != nil {
		return err
	}
	return nil
}
```