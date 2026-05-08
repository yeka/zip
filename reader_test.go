// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zip

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"go4.org/readerutil"
)

type ZipTest struct {
	Name    string
	Source  func() (r io.ReaderAt, size int64) // if non-nil, used instead of testdata/<Name> file
	Comment string
	File    []ZipTestFile
	Error   error // the error that Opening this file should return
}

type ZipTestFile struct {
	Name       string
	Content    []byte // if blank, will attempt to compare against File
	ContentErr error
	File       string // name of file to compare to (relative to testdata/)
	Mtime      string // modified time in format "mm-dd-yy hh:mm:ss"
	Mode       os.FileMode
}

// Caution: The Mtime values found for the test files should correspond to
//          the values listed with unzip -l <zipfile>. However, the values
//          listed by unzip appear to be off by some hours. When creating
//          fresh test files and testing them, this issue is not present.
//          The test files were created in Sydney, so there might be a time
//          zone issue. The time zone information does have to be encoded
//          somewhere, because otherwise unzip -l could not provide a different
//          time from what the archive/zip package provides, but there appears
//          to be no documentation about this.

var tests = []ZipTest{
	{
		Name:    "test.zip",
		Comment: "This is a zipfile comment.",
		File: []ZipTestFile{
			{
				Name:    "test.txt",
				Content: []byte("This is a test text file.\n"),
				Mtime:   "09-05-10 12:12:02",
				Mode:    0644,
			},
			{
				Name:  "gophercolor16x16.png",
				File:  "gophercolor16x16.png",
				Mtime: "09-05-10 15:52:58",
				Mode:  0644,
			},
		},
	},
	{
		Name:    "test-trailing-junk.zip",
		Comment: "This is a zipfile comment.",
		File: []ZipTestFile{
			{
				Name:    "test.txt",
				Content: []byte("This is a test text file.\n"),
				Mtime:   "09-05-10 12:12:02",
				Mode:    0644,
			},
			{
				Name:  "gophercolor16x16.png",
				File:  "gophercolor16x16.png",
				Mtime: "09-05-10 15:52:58",
				Mode:  0644,
			},
		},
	},
	{
		Name:   "r.zip",
		Source: returnRecursiveZip,
		File: []ZipTestFile{
			{
				Name:    "r/r.zip",
				Content: rZipBytes(),
				Mtime:   "03-04-10 00:24:16",
				Mode:    0666,
			},
		},
	},
	{
		Name: "symlink.zip",
		File: []ZipTestFile{
			{
				Name:    "symlink",
				Content: []byte("../target"),
				Mode:    0777 | os.ModeSymlink,
			},
		},
	},
	{
		Name: "readme.zip",
	},
	{
		Name:  "readme.notzip",
		Error: ErrFormat,
	},
	{
		Name: "dd.zip",
		File: []ZipTestFile{
			{
				Name:    "filename",
				Content: []byte("This is a test textfile.\n"),
				Mtime:   "02-02-11 13:06:20",
				Mode:    0666,
			},
		},
	},
	{
		// created in windows XP file manager.
		Name: "winxp.zip",
		File: crossPlatform,
	},
	{
		// created by Zip 3.0 under Linux
		Name: "unix.zip",
		File: crossPlatform,
	},
	{
		// created by Go, before we wrote the "optional" data
		// descriptor signatures (which are required by OS X)
		Name: "go-no-datadesc-sig.zip",
		File: []ZipTestFile{
			{
				Name:    "foo.txt",
				Content: []byte("foo\n"),
				Mtime:   "03-08-12 16:59:10",
				Mode:    0644,
			},
			{
				Name:    "bar.txt",
				Content: []byte("bar\n"),
				Mtime:   "03-08-12 16:59:12",
				Mode:    0644,
			},
		},
	},
	{
		// created by Go, after we wrote the "optional" data
		// descriptor signatures (which are required by OS X)
		Name: "go-with-datadesc-sig.zip",
		File: []ZipTestFile{
			{
				Name:    "foo.txt",
				Content: []byte("foo\n"),
				Mode:    0666,
			},
			{
				Name:    "bar.txt",
				Content: []byte("bar\n"),
				Mode:    0666,
			},
		},
	},
	{
		Name:   "Bad-CRC32-in-data-descriptor",
		Source: returnCorruptCRC32Zip,
		File: []ZipTestFile{
			{
				Name:       "foo.txt",
				Content:    []byte("foo\n"),
				Mode:       0666,
				ContentErr: ErrChecksum,
			},
			{
				Name:    "bar.txt",
				Content: []byte("bar\n"),
				Mode:    0666,
			},
		},
	},
	// Tests that we verify (and accept valid) crc32s on files
	// with crc32s in their file header (not in data descriptors)
	{
		Name: "crc32-not-streamed.zip",
		File: []ZipTestFile{
			{
				Name:    "foo.txt",
				Content: []byte("foo\n"),
				Mtime:   "03-08-12 16:59:10",
				Mode:    0644,
			},
			{
				Name:    "bar.txt",
				Content: []byte("bar\n"),
				Mtime:   "03-08-12 16:59:12",
				Mode:    0644,
			},
		},
	},
	// Tests that we verify (and reject invalid) crc32s on files
	// with crc32s in their file header (not in data descriptors)
	{
		Name:   "crc32-not-streamed.zip",
		Source: returnCorruptNotStreamedZip,
		File: []ZipTestFile{
			{
				Name:       "foo.txt",
				Content:    []byte("foo\n"),
				Mtime:      "03-08-12 16:59:10",
				Mode:       0644,
				ContentErr: ErrChecksum,
			},
			{
				Name:    "bar.txt",
				Content: []byte("bar\n"),
				Mtime:   "03-08-12 16:59:12",
				Mode:    0644,
			},
		},
	},
	{
		Name: "zip64.zip",
		File: []ZipTestFile{
			{
				Name:    "README",
				Content: []byte("This small file is in ZIP64 format.\n"),
				Mtime:   "08-10-12 14:33:32",
				Mode:    0644,
			},
		},
	},
	// Another zip64 file with different Extras fields. (golang.org/issue/7069)
	{
		Name: "zip64-2.zip",
		File: []ZipTestFile{
			{
				Name:    "README",
				Content: []byte("This small file is in ZIP64 format.\n"),
				Mtime:   "08-10-12 14:33:32",
				Mode:    0644,
			},
		},
	},
}

var crossPlatform = []ZipTestFile{
	{
		Name:    "hello",
		Content: []byte("world \r\n"),
		Mode:    0666,
	},
	{
		Name:    "dir/bar",
		Content: []byte("foo \r\n"),
		Mode:    0666,
	},
	{
		Name:    "dir/empty/",
		Content: []byte{},
		Mode:    os.ModeDir | 0777,
	},
	{
		Name:    "readonly",
		Content: []byte("important \r\n"),
		Mode:    0444,
	},
}

func TestReader(t *testing.T) {
	for _, zt := range tests {
		readTestZip(t, zt)
	}
}

func readTestZip(t *testing.T, zt ZipTest) {
	var z *Reader
	var err error
	if zt.Source != nil {
		rat, size := zt.Source()
		z, err = NewReader(rat, size)
	} else {
		var rc *ReadCloser
		rc, err = OpenReader(filepath.Join("testdata", zt.Name))
		if err == nil {
			defer rc.Close()
			z = &rc.Reader
		}
	}
	if err != zt.Error {
		t.Errorf("%s: error=%v, want %v", zt.Name, err, zt.Error)
		return
	}

	// bail if file is not zip
	if err == ErrFormat {
		return
	}

	// bail here if no Files expected to be tested
	// (there may actually be files in the zip, but we don't care)
	if zt.File == nil {
		return
	}

	if z.Comment != zt.Comment {
		t.Errorf("%s: comment=%q, want %q", zt.Name, z.Comment, zt.Comment)
	}
	if len(z.File) != len(zt.File) {
		t.Fatalf("%s: file count=%d, want %d", zt.Name, len(z.File), len(zt.File))
	}

	// test read of each file
	for i, ft := range zt.File {
		readTestFile(t, zt, ft, z.File[i])
	}

	// test simultaneous reads
	n := 0
	done := make(chan bool)
	for i := 0; i < 5; i++ {
		for j, ft := range zt.File {
			go func(j int, ft ZipTestFile) {
				readTestFile(t, zt, ft, z.File[j])
				done <- true
			}(j, ft)
			n++
		}
	}
	for ; n > 0; n-- {
		<-done
	}
}

func readTestFile(t *testing.T, zt ZipTest, ft ZipTestFile, f *File) {
	if f.Name != ft.Name {
		t.Errorf("%s: name=%q, want %q", zt.Name, f.Name, ft.Name)
	}

	if ft.Mtime != "" {
		mtime, err := time.Parse("01-02-06 15:04:05", ft.Mtime)
		if err != nil {
			t.Error(err)
			return
		}
		if ft := f.ModTime(); !ft.Equal(mtime) {
			t.Errorf("%s: %s: mtime=%s, want %s", zt.Name, f.Name, ft, mtime)
		}
	}

	testFileMode(t, zt.Name, f, ft.Mode)

	var b bytes.Buffer
	r, err := f.Open()
	if err != nil {
		t.Errorf("%s: %v", zt.Name, err)
		return
	}

	_, err = io.Copy(&b, r)
	if err != ft.ContentErr {
		t.Errorf("%s: copying contents: %v (want %v)", zt.Name, err, ft.ContentErr)
	}
	if err != nil {
		return
	}
	r.Close()

	size := uint64(f.UncompressedSize)
	if size == uint32max {
		size = f.UncompressedSize64
	}
	if g := uint64(b.Len()); g != size {
		t.Errorf("%v: read %v bytes but f.UncompressedSize == %v", f.Name, g, size)
	}

	var c []byte
	if ft.Content != nil {
		c = ft.Content
	} else if c, err = ioutil.ReadFile("testdata/" + ft.File); err != nil {
		t.Error(err)
		return
	}

	if b.Len() != len(c) {
		t.Errorf("%s: len=%d, want %d", f.Name, b.Len(), len(c))
		return
	}

	for i, b := range b.Bytes() {
		if b != c[i] {
			t.Errorf("%s: content[%d]=%q want %q", f.Name, i, b, c[i])
			return
		}
	}
}

func testFileMode(t *testing.T, zipName string, f *File, want os.FileMode) {
	mode := f.Mode()
	if want == 0 {
		t.Errorf("%s: %s mode: got %v, want none", zipName, f.Name, mode)
	} else if mode != want {
		t.Errorf("%s: %s mode: want %v, got %v", zipName, f.Name, want, mode)
	}
}

func TestInvalidFiles(t *testing.T) {
	const size = 1024 * 70 // 70kb
	b := make([]byte, size)

	// zeroes
	_, err := NewReader(bytes.NewReader(b), size)
	if err != ErrFormat {
		t.Errorf("zeroes: error=%v, want %v", err, ErrFormat)
	}

	// repeated directoryEndSignatures
	sig := make([]byte, 4)
	binary.LittleEndian.PutUint32(sig, directoryEndSignature)
	for i := 0; i < size-4; i += 4 {
		copy(b[i:i+4], sig)
	}
	_, err = NewReader(bytes.NewReader(b), size)
	if err != ErrFormat {
		t.Errorf("sigs: error=%v, want %v", err, ErrFormat)
	}
}

func TestNewMultipartReader(t *testing.T) {
	expectedFilesPng := map[string]int{
		"Users/nikko/Downloads/HeaderRight.png": 152499,
	}
	testCases := []struct {
		name string
		// the order of the path matters, .zip should be last
		paths    []string
		password string
		// key val of filename and content len
		files           map[string]int
		expectedReadErr string
		expectedOpenErr string
	}{
		{
			name: "success non-protected",
			paths: []string{
				"./testdata/multipart/datasplit.z01",
				"./testdata/multipart/datasplit.z02",
				"./testdata/multipart/datasplit.zip",
			},
			files: expectedFilesPng,
		},
		{
			name: "success non-protected - z64",
			paths: []string{
				"./testdata/multipart/datasplit-z64.z01",
				"./testdata/multipart/datasplit-z64.z02",
				"./testdata/multipart/datasplit-z64.zip",
			},
			files: expectedFilesPng,
		},
		{
			name: "success protected",
			paths: []string{
				"./testdata/multipart/datasplit-protected.z01",
				"./testdata/multipart/datasplit-protected.z02",
				"./testdata/multipart/datasplit-protected.zip",
			},
			password: "test123",
			files:    expectedFilesPng,
		},
		{
			name: "success protected - z64",
			paths: []string{
				"./testdata/multipart/datasplit-protected-z64.z01",
				"./testdata/multipart/datasplit-protected-z64.z02",
				"./testdata/multipart/datasplit-protected-z64.zip",
			},
			password: "test123",
			files:    expectedFilesPng,
		},
		{
			name: "success protected - multifiles multipart",
			paths: []string{
				"./testdata/multipart/datasplit-protected-multifiles.z01",
				"./testdata/multipart/datasplit-protected-multifiles.z02",
				"./testdata/multipart/datasplit-protected-multifiles.zip",
			},
			files: map[string]int{
				"file1.txt": 53337,
				"file2.txt": 53337,
				"file3.txt": 53337,
			},
			password: "golang",
		},
		{
			name: "success non-protected - multifiles multipart",
			paths: []string{
				"./testdata/multipart/datasplit-multifiles.z01",
				"./testdata/multipart/datasplit-multifiles.z02",
				"./testdata/multipart/datasplit-multifiles.zip",
			},
			files: map[string]int{
				"file1.txt": 53337,
				"file2.txt": 53337,
				"file3.txt": 53337,
			},
		},
		{
			name: "failed protected - incorrect password",
			paths: []string{
				"./testdata/multipart/datasplit-protected.z01",
				"./testdata/multipart/datasplit-protected.z02",
				"./testdata/multipart/datasplit-protected.zip",
			},
			password:        "wrong",
			files:           expectedFilesPng,
			expectedReadErr: "flate: corrupt input before offset 1",
		},
		{
			name: "failed protected - no password supplied",
			paths: []string{
				"./testdata/multipart/datasplit-protected.z01",
				"./testdata/multipart/datasplit-protected.z02",
				"./testdata/multipart/datasplit-protected.zip",
			},
			files:           expectedFilesPng,
			expectedOpenErr: "zip: invalid password",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parts := []readerutil.SizeReaderAt{}
			for _, path := range tc.paths {
				f, err := os.Open(path)
				if err != nil {
					t.Fatalf("Failed to open multipart zip part %s: %v", path, err)
				}
				defer f.Close()
				stat, err := f.Stat()
				if err != nil {
					t.Fatalf("Failed to stat multipart zip file: %v", err)
				}
				parts = append(parts, io.NewSectionReader(f, 0, stat.Size()))
			}

			zr, err := NewMultipartReader(parts)
			if err != nil {
				t.Fatalf("Failed to create multipart reader: %v", err)
			}

			if got, want := len(zr.File), len(tc.files); got != want {
				t.Fatalf("multipart reader returned %d files, want %d", got, want)
			}

			for _, f := range zr.File {
				if _, ok := tc.files[f.Name]; !ok {
					t.Fatalf("unexpected entry name: got %q", f.Name)
				}

				if tc.password != "" {
					if !f.IsEncrypted() {
						t.Fatalf("expected file %q to be encrypted", f.Name)
					}
					f.SetPassword(tc.password)
				}

				r, err := f.Open()

				if tc.expectedOpenErr != "" {
					if err.Error() != tc.expectedOpenErr {
						t.Fatalf("expected error %q, got %q", tc.expectedOpenErr, err)
					}
					return
				}

				if err != nil {
					t.Fatalf("Failed to open %s: %v", f.Name, err)
				}
				defer r.Close()

				buf, err := io.ReadAll(r)

				if tc.expectedReadErr != "" {
					if err.Error() != tc.expectedReadErr {
						t.Fatalf("expected error %q, got %q", tc.expectedReadErr, err)
					}
					return
				}

				if got, want := len(buf), int(f.UncompressedSize64); got != want {
					t.Fatalf("read %d bytes, want %d", got, want)
				}

				if got, want := len(buf), tc.files[f.Name]; got != want {
					t.Fatalf("unexpected size for %s: got %d, want %d", f.Name, got, want)
				}

			}
		})
	}
}

func messWith(fileName string, corrupter func(b []byte)) (r io.ReaderAt, size int64) {
	data, err := ioutil.ReadFile(filepath.Join("testdata", fileName))
	if err != nil {
		panic("Error reading " + fileName + ": " + err.Error())
	}
	corrupter(data)
	return bytes.NewReader(data), int64(len(data))
}

func returnCorruptCRC32Zip() (r io.ReaderAt, size int64) {
	return messWith("go-with-datadesc-sig.zip", func(b []byte) {
		// Corrupt one of the CRC32s in the data descriptor:
		b[0x2d]++
	})
}

func returnCorruptNotStreamedZip() (r io.ReaderAt, size int64) {
	return messWith("crc32-not-streamed.zip", func(b []byte) {
		// Corrupt foo.txt's final crc32 byte, in both
		// the file header and TOC. (0x7e -> 0x7f)
		b[0x11]++
		b[0x9d]++

		// TODO(bradfitz): add a new test that only corrupts
		// one of these values, and verify that that's also an
		// error. Currently, the reader code doesn't verify the
		// fileheader and TOC's crc32 match if they're both
		// non-zero and only the second line above, the TOC,
		// is what matters.
	})
}

// rZipBytes returns the bytes of a recursive zip file, without
// putting it on disk and triggering certain virus scanners.
func rZipBytes() []byte {
	s := `
0000000 50 4b 03 04 14 00 00 00 08 00 08 03 64 3c f9 f4
0000010 89 64 48 01 00 00 b8 01 00 00 07 00 00 00 72 2f
0000020 72 2e 7a 69 70 00 25 00 da ff 50 4b 03 04 14 00
0000030 00 00 08 00 08 03 64 3c f9 f4 89 64 48 01 00 00
0000040 b8 01 00 00 07 00 00 00 72 2f 72 2e 7a 69 70 00
0000050 2f 00 d0 ff 00 25 00 da ff 50 4b 03 04 14 00 00
0000060 00 08 00 08 03 64 3c f9 f4 89 64 48 01 00 00 b8
0000070 01 00 00 07 00 00 00 72 2f 72 2e 7a 69 70 00 2f
0000080 00 d0 ff c2 54 8e 57 39 00 05 00 fa ff c2 54 8e
0000090 57 39 00 05 00 fa ff 00 05 00 fa ff 00 14 00 eb
00000a0 ff c2 54 8e 57 39 00 05 00 fa ff 00 05 00 fa ff
00000b0 00 14 00 eb ff 42 88 21 c4 00 00 14 00 eb ff 42
00000c0 88 21 c4 00 00 14 00 eb ff 42 88 21 c4 00 00 14
00000d0 00 eb ff 42 88 21 c4 00 00 14 00 eb ff 42 88 21
00000e0 c4 00 00 00 00 ff ff 00 00 00 ff ff 00 34 00 cb
00000f0 ff 42 88 21 c4 00 00 00 00 ff ff 00 00 00 ff ff
0000100 00 34 00 cb ff 42 e8 21 5e 0f 00 00 00 ff ff 0a
0000110 f0 66 64 12 61 c0 15 dc e8 a0 48 bf 48 af 2a b3
0000120 20 c0 9b 95 0d c4 67 04 42 53 06 06 06 40 00 06
0000130 00 f9 ff 6d 01 00 00 00 00 42 e8 21 5e 0f 00 00
0000140 00 ff ff 0a f0 66 64 12 61 c0 15 dc e8 a0 48 bf
0000150 48 af 2a b3 20 c0 9b 95 0d c4 67 04 42 53 06 06
0000160 06 40 00 06 00 f9 ff 6d 01 00 00 00 00 50 4b 01
0000170 02 14 00 14 00 00 00 08 00 08 03 64 3c f9 f4 89
0000180 64 48 01 00 00 b8 01 00 00 07 00 00 00 00 00 00
0000190 00 00 00 00 00 00 00 00 00 00 00 72 2f 72 2e 7a
00001a0 69 70 50 4b 05 06 00 00 00 00 01 00 01 00 35 00
00001b0 00 00 6d 01 00 00 00 00`
	s = regexp.MustCompile(`[0-9a-f]{7}`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, "")
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func returnRecursiveZip() (r io.ReaderAt, size int64) {
	b := rZipBytes()
	return bytes.NewReader(b), int64(len(b))
}

func TestIssue8186(t *testing.T) {
	// Directory headers & data found in the TOC of a JAR file.
	dirEnts := []string{
		"PK\x01\x02\n\x00\n\x00\x00\b\x00\x004\x9d3?\xaa\x1b\x06\xf0\x81\x02\x00\x00\x81\x02\x00\x00-\x00\x05\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00res/drawable-xhdpi-v4/ic_actionbar_accept.png\xfe\xca\x00\x00\x00",
		"PK\x01\x02\n\x00\n\x00\x00\b\x00\x004\x9d3?\x90K\x89\xc7t\n\x00\x00t\n\x00\x00\x0e\x00\x03\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\xd1\x02\x00\x00resources.arsc\x00\x00\x00",
		"PK\x01\x02\x14\x00\x14\x00\b\b\b\x004\x9d3?\xff$\x18\xed3\x03\x00\x00\xb4\b\x00\x00\x13\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00t\r\x00\x00AndroidManifest.xml",
		"PK\x01\x02\x14\x00\x14\x00\b\b\b\x004\x9d3?\x14\xc5K\xab\x192\x02\x00\xc8\xcd\x04\x00\v\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\xe8\x10\x00\x00classes.dex",
		"PK\x01\x02\x14\x00\x14\x00\b\b\b\x004\x9d3?E\x96\nD\xac\x01\x00\x00P\x03\x00\x00&\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00:C\x02\x00res/layout/actionbar_set_wallpaper.xml",
		"PK\x01\x02\x14\x00\x14\x00\b\b\b\x004\x9d3?Ļ\x14\xe3\xd8\x01\x00\x00\xd8\x03\x00\x00 \x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00:E\x02\x00res/layout/wallpaper_cropper.xml",
		"PK\x01\x02\x14\x00\x14\x00\b\b\b\x004\x9d3?}\xc1\x15\x9eZ\x01\x00\x00!\x02\x00\x00\x14\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00`G\x02\x00META-INF/MANIFEST.MF",
		"PK\x01\x02\x14\x00\x14\x00\b\b\b\x004\x9d3?\xe6\x98Ьo\x01\x00\x00\x84\x02\x00\x00\x10\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\xfcH\x02\x00META-INF/CERT.SF",
		"PK\x01\x02\x14\x00\x14\x00\b\b\b\x004\x9d3?\xbfP\x96b\x86\x04\x00\x00\xb2\x06\x00\x00\x11\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\xa9J\x02\x00META-INF/CERT.RSA",
	}
	for i, s := range dirEnts {
		var f File
		err := readDirectoryHeader(&f, strings.NewReader(s))
		if err != nil {
			t.Errorf("error reading #%d: %v", i, err)
		}
	}
}

// Verify we return ErrUnexpectedEOF when length is short.
func TestIssue10957(t *testing.T) {
	data := []byte("PK\x03\x040000000PK\x01\x0200000" +
		"0000000000000000000\x00" +
		"\x00\x00\x00\x00\x00000000000000PK\x01" +
		"\x020000000000000000000" +
		"00000\v\x00\x00\x00\x00\x00000000000" +
		"00000000000000PK\x01\x0200" +
		"00000000000000000000" +
		"00\v\x00\x00\x00\x00\x00000000000000" +
		"00000000000PK\x01\x020000<" +
		"0\x00\x0000000000000000\v\x00\v" +
		"\x00\x00\x00\x00\x0000000000\x00\x00\x00\x00000" +
		"00000000PK\x01\x0200000000" +
		"0000000000000000\v\x00\x00\x00" +
		"\x00\x0000PK\x05\x06000000\x05\x000000" +
		"\v\x00\x00\x00\x00\x00")
	z, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range z.File {
		r, err := f.Open()
		if err != nil {
			continue
		}
		if f.UncompressedSize64 < 1e6 {
			n, err := io.Copy(ioutil.Discard, r)
			if i == 3 && err != io.ErrUnexpectedEOF {
				t.Errorf("File[3] error = %v; want io.ErrUnexpectedEOF", err)
			}
			if err == nil && uint64(n) != f.UncompressedSize64 {
				t.Errorf("file %d: bad size: copied=%d; want=%d", i, n, f.UncompressedSize64)
			}
		}
		r.Close()
	}
}

// Verify the number of files is sane.
func TestIssue10956(t *testing.T) {
	data := []byte("PK\x06\x06PK\x06\a0000\x00\x00\x00\x00\x00\x00\x00\x00" +
		"0000PK\x05\x06000000000000" +
		"0000\v\x00000\x00\x00\x00\x00\x00\x00\x000")
	_, err := NewReader(bytes.NewReader(data), int64(len(data)))
	const want = "TOC declares impossible 3472328296227680304 files in 57 byte"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v; want %q", err, want)
	}
}

// Verify we return ErrUnexpectedEOF when reading truncated data descriptor.
func TestIssue11146(t *testing.T) {
	data := []byte("PK\x03\x040000000000000000" +
		"000000\x01\x00\x00\x000\x01\x00\x00\xff\xff0000" +
		"0000000000000000PK\x01\x02" +
		"0000\b0\b\x00000000000000" +
		"\x00\x00\x00\x00\x00\x00\x00\x00\x00\x000000PK\x05\x06\x00\x00" +
		"\x00\x0000\x01\x0000008\x00\x00\x00\x00\x00")
	z, err := NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	r, err := z.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = ioutil.ReadAll(r)
	if err != io.ErrUnexpectedEOF {
		t.Errorf("File[0] error = %v; want io.ErrUnexpectedEOF", err)
	}
	r.Close()
}

func writeLocalFileHeader(buf *bytes.Buffer, name, content string) {
	binary.Write(buf, binary.LittleEndian, uint32(fileHeaderSignature))
	binary.Write(buf, binary.LittleEndian, uint16(20))                          // version needed
	binary.Write(buf, binary.LittleEndian, uint16(0))                           // flags
	binary.Write(buf, binary.LittleEndian, uint16(Store))                       // method
	binary.Write(buf, binary.LittleEndian, uint16(0))                           // mod time
	binary.Write(buf, binary.LittleEndian, uint16(0))                           // mod date
	binary.Write(buf, binary.LittleEndian, crc32.ChecksumIEEE([]byte(content))) // CRC-32
	binary.Write(buf, binary.LittleEndian, uint32(len(content)))                // compressed size
	binary.Write(buf, binary.LittleEndian, uint32(len(content)))                // uncompressed size
	binary.Write(buf, binary.LittleEndian, uint16(len(name)))                   // filename length
	binary.Write(buf, binary.LittleEndian, uint16(0))                           // extra length
	buf.WriteString(name)
}

func writeCentralDirHeaderAsymmetric(buf *bytes.Buffer, name, content string, realOffset uint64) {
	const extraLen = 4 + 8 // tag+size header + 8-byte offset only

	binary.Write(buf, binary.LittleEndian, uint32(directoryHeaderSignature))
	binary.Write(buf, binary.LittleEndian, uint16(20))                          // version made by
	binary.Write(buf, binary.LittleEndian, uint16(45))                          // version needed (zip64)
	binary.Write(buf, binary.LittleEndian, uint16(0))                           // flags
	binary.Write(buf, binary.LittleEndian, uint16(Store))                       // method
	binary.Write(buf, binary.LittleEndian, uint16(0))                           // mod time
	binary.Write(buf, binary.LittleEndian, uint16(0))                           // mod date
	binary.Write(buf, binary.LittleEndian, crc32.ChecksumIEEE([]byte(content))) // CRC-32
	binary.Write(buf, binary.LittleEndian, uint32(len(content)))                // compressed size (NOT sentinel)
	binary.Write(buf, binary.LittleEndian, uint32(len(content)))                // uncompressed size (NOT sentinel)
	binary.Write(buf, binary.LittleEndian, uint16(len(name)))                   // filename length
	binary.Write(buf, binary.LittleEndian, uint16(extraLen))                    // extra length
	binary.Write(buf, binary.LittleEndian, uint16(0))                           // comment length
	binary.Write(buf, binary.LittleEndian, uint16(0))                           // disk number start
	binary.Write(buf, binary.LittleEndian, uint16(0))                           // internal attrs
	binary.Write(buf, binary.LittleEndian, uint32(0))                           // external attrs
	binary.Write(buf, binary.LittleEndian, uint32(uint32max))                   // header offset = SENTINEL
	buf.WriteString(name)

	// Zip64 extra block: only the 8-byte offset.
	binary.Write(buf, binary.LittleEndian, uint16(zip64ExtraId))
	binary.Write(buf, binary.LittleEndian, uint16(8))
	binary.Write(buf, binary.LittleEndian, realOffset)
}

func writeCentralDirHeaderTruncatedExtra(buf *bytes.Buffer, name, content string, realOffset uint64) {
	const extraLen = 4 // tag+size header only, no payload

	binary.Write(buf, binary.LittleEndian, uint32(directoryHeaderSignature))
	binary.Write(buf, binary.LittleEndian, uint16(20))
	binary.Write(buf, binary.LittleEndian, uint16(45))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, uint16(Store))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, crc32.ChecksumIEEE([]byte(content)))
	binary.Write(buf, binary.LittleEndian, uint32(len(content)))
	binary.Write(buf, binary.LittleEndian, uint32(len(content)))
	binary.Write(buf, binary.LittleEndian, uint16(len(name)))
	binary.Write(buf, binary.LittleEndian, uint16(extraLen))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, uint16(0))
	binary.Write(buf, binary.LittleEndian, uint32(0))
	binary.Write(buf, binary.LittleEndian, uint32(uint32max))
	buf.WriteString(name)

	binary.Write(buf, binary.LittleEndian, uint16(zip64ExtraId))
	binary.Write(buf, binary.LittleEndian, uint16(0)) // claims promotion, supplies nothing
}

func writeEOCD(buf *bytes.Buffer, cdOffset, cdSize uint64, entries uint16) {
	binary.Write(buf, binary.LittleEndian, uint32(directoryEndSignature))
	binary.Write(buf, binary.LittleEndian, uint16(0))        // disk number
	binary.Write(buf, binary.LittleEndian, uint16(0))        // disk with start of CD
	binary.Write(buf, binary.LittleEndian, uint16(entries))  // entries on this disk
	binary.Write(buf, binary.LittleEndian, uint16(entries))  // total entries
	binary.Write(buf, binary.LittleEndian, uint32(cdSize))   // CD size
	binary.Write(buf, binary.LittleEndian, uint32(cdOffset)) // CD offset
	binary.Write(buf, binary.LittleEndian, uint16(0))        // comment length
}

// TestZip64AsymmetricPromotion verifies that the central-directory zip64
// extra-block parser honors APPNOTE 4.5.3: only the fields whose 32-bit
// counterpart was sentinel-promoted (0xFFFFFFFF) appear in the extra
// block, in field order.
//
// The fixture is a hand-rolled archive with one stored entry "asym"
// containing "hi", where the 32-bit local-header offset is set to the
// sentinel and the zip64 extra contains *only* the 8-byte real offset.
// Sizes stay in 32-bit form. Before the sentinel-aware fix, the parser
// would consume the offset bytes as UncompressedSize64 and leave
// headerOffset stuck at 0xFFFFFFFF.
func TestZip64AsymmetricPromotion(t *testing.T) {
	const (
		filename = "asym"
		content  = "hi"
	)

	var buf bytes.Buffer

	// Local file header at offset 0.
	localHeaderOffset := uint64(buf.Len())
	writeLocalFileHeader(&buf, filename, content)
	buf.WriteString(content)

	// Central directory header. headerOffset field is sentinel; real
	// offset goes in the zip64 extra. Sizes are NOT sentinel.
	cdOffset := uint64(buf.Len())
	writeCentralDirHeaderAsymmetric(&buf, filename, content, localHeaderOffset)
	cdSize := uint64(buf.Len()) - cdOffset

	// EOCD. No zip64 EOCD needed — only the offset field was promoted,
	// and the EOCD itself records cdOffset which fits in 32 bits.
	writeEOCD(&buf, cdOffset, cdSize, 1)

	zr, err := NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if got := len(zr.File); got != 1 {
		t.Fatalf("entry count: got %d, want 1", got)
	}
	f := zr.File[0]
	if f.Name != filename {
		t.Errorf("Name: got %q, want %q", f.Name, filename)
	}
	// The bug under test would leave headerOffset at uint32max.
	if f.headerOffset != int64(localHeaderOffset) {
		t.Errorf("headerOffset: got %d, want %d (still sentinel? %v)",
			f.headerOffset, localHeaderOffset, f.headerOffset == int64(uint32max))
	}
	if f.UncompressedSize64 != uint64(len(content)) {
		t.Errorf("UncompressedSize64: got %d, want %d", f.UncompressedSize64, len(content))
	}

	rc, err := f.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != content {
		t.Errorf("contents: got %q, want %q", got, content)
	}
}

// TestZip64AsymmetricPromotion_TruncatedExtra verifies that a malformed
// archive — one that claims sentinel promotion but provides too few
// bytes in the zip64 extra — is rejected with ErrFormat instead of
// silently corrupting the parsed offsets.
func TestZip64AsymmetricPromotion_TruncatedExtra(t *testing.T) {
	const (
		filename = "trunc"
		content  = "hi"
	)

	var buf bytes.Buffer
	localHeaderOffset := uint64(buf.Len())
	writeLocalFileHeader(&buf, filename, content)
	buf.WriteString(content)

	// Central directory entry with sentinel offset but a zip64 extra
	// of size 0 — i.e. it claims promotion but supplies no offset.
	cdOffset := uint64(buf.Len())
	writeCentralDirHeaderTruncatedExtra(&buf, filename, content, localHeaderOffset)
	cdSize := uint64(buf.Len()) - cdOffset
	writeEOCD(&buf, cdOffset, cdSize, 1)

	_, err := NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != ErrFormat {
		t.Fatalf("got %v, want ErrFormat", err)
	}
}
